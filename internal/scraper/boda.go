package scraper

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// ListNews fetches the news list page and parses items.
// Tries .htm first, then .html if that fails.
func (s *BodaSite) ListNews(slug string) ([]model.NewsItem, error) {
	return s.ListNewsPage(slug, 1)
}

// ListNewsWithDates fetches news and fills in missing dates from detail pages.
func (s *BodaSite) ListNewsWithDates(slug string) ([]model.NewsItem, error) {
	items, err := s.ListNewsPage(slug, 1)
	if err != nil {
		return nil, err
	}
	fillMissingDates(items)
	return items, nil
}

// fillMissingDates fetches detail pages for items missing dates.
func fillMissingDates(items []model.NewsItem) {
	for i := range items {
		if items[i].Date != "" {
			continue
		}
		body, err := fetch(items[i].URL)
		if err != nil {
			continue
		}
		art, err := parseBodaArticle(body, "")
		if err != nil || art.Date == "" {
			continue
		}
		items[i].Date = art.Date
	}
}

// ListNewsPage fetches a specific page of news.
// page 1 = first page (slug.htm), page 2+ = slug/{total-page+2-page}.htm
func (s *BodaSite) ListNewsPage(slug string, page int) ([]model.NewsItem, error) {
	totalPages := 0
	if page > 1 && s.PageURL != nil {
		// Some static sites number generated files from newest to oldest.
		// Let the site's URL function map the user-facing page number.
		totalPages, _ = s.GetTotalPages(slug)
	}
	listURL := s.listPageURL(slug, page, totalPages, ".htm")

	body, err := fetch(listURL)
	if err != nil {
		// Try .html extension
		listURL = s.listPageURL(slug, page, totalPages, ".html")
		body, err = fetch(listURL)
		if err != nil {
			return nil, fmt.Errorf("tried .htm and .html: %w", err)
		}
	}
	if s.ParseFunc != nil {
		return s.ParseFunc(body, s.BaseURL)
	}
	return parseBodaNewsList(body, s.BaseURL)
}

// GetTotalPages extracts total page count from a list page.
func (s *BodaSite) GetTotalPages(slug string) (int, error) {
	listURL := s.listPageURL(slug, 1, 0, ".htm")
	body, err := fetch(listURL)
	if err != nil {
		listURL = s.listPageURL(slug, 1, 0, ".html")
		body, err = fetch(listURL)
		if err != nil {
			return 0, err
		}
	}
	return parseTotalPages(body), nil
}

func (s *BodaSite) listPageURL(slug string, page, totalPages int, extension string) string {
	if s.PageURL != nil {
		return s.PageURL(s.BaseURL, slug, page, totalPages, extension)
	}
	if page <= 1 {
		return fmt.Sprintf("%s/%s%s", s.BaseURL, slug, extension)
	}
	return fmt.Sprintf("%s/%s/%d%s", s.BaseURL, slug, page, extension)
}

// ListNewsRange fetches news from a date range by paginating.
// from/to format: "2026-01-01", "2026-06-14", etc.
func (s *BodaSite) ListNewsRange(slug string, from, to string, maxPages int) ([]model.NewsItem, error) {
	var allItems []model.NewsItem
	seen := make(map[string]bool)

	fromTime := parseDate(from)
	toTime := parseDate(to)

	// Fetch page 1 first
	items, err := s.ListNewsPage(slug, 1)
	if err != nil {
		return nil, err
	}

	totalPages, _ := s.GetTotalPages(slug)
	if totalPages == 0 {
		totalPages = 10 // fallback
	}
	if maxPages > 0 && totalPages > maxPages {
		totalPages = maxPages
	}

	for _, item := range items {
		t := parseDate(item.Date)
		if !t.IsZero() && (t.Before(fromTime) || t.After(toTime.Add(24*time.Hour))) {
			continue
		}
		if !seen[item.URL] {
			seen[item.URL] = true
			allItems = append(allItems, item)
		}
	}

	// Check if we need more pages
	if len(items) > 0 {
		lastDate := parseDate(items[len(items)-1].Date)
		if lastDate.IsZero() || !lastDate.Before(fromTime) {
			// Need more pages. The legacy default URL scheme fetches generated
			// files from oldest to newest; custom URL functions use logical page
			// numbers and decide the file order themselves.
			for p := 2; p <= totalPages; p++ {
				realPage := totalPages + 2 - p
				if s.PageURL != nil {
					// A custom URL function receives user-facing page numbers;
					// it handles any reverse numbering internally.
					realPage = p
				}
				pageItems, err := s.ListNewsPage(slug, realPage)
				if err != nil || len(pageItems) == 0 {
					break
				}

				shouldStop := false
				for _, item := range pageItems {
					t := parseDate(item.Date)
					if !t.IsZero() && t.Before(fromTime) {
						shouldStop = true
						break
					}
					if !t.IsZero() && t.After(toTime.Add(24*time.Hour)) {
						continue
					}
					if !seen[item.URL] {
						seen[item.URL] = true
						allItems = append(allItems, item)
					}
				}
				if shouldStop {
					break
				}
				time.Sleep(300 * time.Millisecond)
			}
		}
	}

	return allItems, nil
}

// parseTotalPages extracts the total page count from pagination HTML.
func parseTotalPages(html []byte) int {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return 0
	}

	// Pattern 1: "共X页" or "/X页"
	totalText := doc.Find(".p_t").Text()
	if strings.Contains(totalText, "/") {
		parts := strings.Split(totalText, "/")
		for _, p := range parts {
			p = strings.TrimSpace(strings.TrimSuffix(p, "页"))
			if n, err := fmt.Sscanf(p, "%d"); err == nil && n > 0 {
				// parse manually
			}
		}
	}

	// Pattern 2: Last page link
	// <span class="p_last p_fun"><a href="1.htm">尾页</a></span>
	lastLink := doc.Find(".p_last a, .p_last").AttrOr("href", "")
	if lastLink == "" {
		// Try text content of p_last
		lastLink = doc.Find(".p_last").Text()
	}

	// Pattern 3: Find the highest page number in pagination links
	maxPage := 0
	doc.Find(".p_no a, .p_pages a").Each(func(i int, sel *goquery.Selection) {
		href, _ := sel.Attr("href")
		text := strings.TrimSpace(sel.Text())
		// Try to extract page number from href or text
		for _, s := range []string{href, text} {
			s = strings.TrimSuffix(strings.TrimSuffix(s, ".htm"), ".html")
			// Extract trailing number
			for j := len(s) - 1; j >= 0; j-- {
				if s[j] < '0' || s[j] > '9' {
					if j < len(s)-1 {
						num := 0
						fmt.Sscanf(s[j+1:], "%d", &num)
						if num > maxPage {
							maxPage = num
						}
					}
					break
				}
			}
		}
	})

	if maxPage > 0 {
		return maxPage
	}

	// Pattern 4: "共X条每页Y条" format, calculate pages
	pageInfo := doc.Find(".paging, .page, .p_pages").Text()
	if strings.Contains(pageInfo, "共") && strings.Contains(pageInfo, "每页") {
		total := 0
		perPage := 0
		fmt.Sscanf(pageInfo, "共%d条每页%d条", &total, &perPage)
		if total > 0 && perPage > 0 {
			return (total + perPage - 1) / perPage
		}
	}

	return 0
}

// GetArticle fetches a single article page and parses its content.
func (s *BodaSite) GetArticle(articleURL string) (*model.Article, error) {
	art, _, err := s.GetArticleSnapshot(articleURL)
	return art, err
}

// GetArticleSnapshot fetches and parses an article while retaining the
// original response body. The snapshot is used by archival clients that need
// to preserve both the source page and the normalized article fields.
func (s *BodaSite) GetArticleSnapshot(articleURL string) (*model.Article, []byte, error) {
	body, err := fetch(articleURL)
	if err != nil {
		return nil, nil, err
	}
	art, err := parseBodaArticle(body, s.BaseURL)
	if err != nil {
		return nil, nil, err
	}
	art.RawHTML = string(body)
	return art, body, nil
}

// navSelectors are CSS selectors for non-content areas to exclude.
var navSelectors = []string{
	"header", "nav", "footer", ".header", ".nav", ".footer",
	".menu", ".top", ".share", ".search", ".crumbs",
	".swiper", ".slider", ".gotop", "script", "style",
	".banner", ".slide", ".carousel", ".focus", ".hero",
	".sidebar", ".side", ".widget", ".login", ".user-info",
}

// cleanTitle removes date prefixes and other noise from titles.
func cleanTitle(title string) string {
	// Collapse newlines and excessive whitespace into single space
	title = strings.Join(strings.Fields(title), " ")
	// Remove date prefixes like "07/16", "2025-08-12", "06-01", "01 2026.06"
	datePrefixes := []string{
		`^\d{2}/\d{2}`,          // 07/16
		`^\d{4}-\d{2}-\d{2}`,    // 2025-08-12
		`^\d{2}\s+\d{4}\.\d{2}`, // 01 2026.06
	}
	for _, pattern := range datePrefixes {
		re := regexp.MustCompile(pattern)
		title = re.ReplaceAllString(title, "")
	}
	// Remove trailing date patterns (full and short)
	trailingDates := []string{
		`\s*\d{4}[-/年.]\d{1,2}[-/月.]\d{1,2}[日]?\s*$`, // 2025-08-12
		`\s+\d{1,2}-\d{1,2}\s*$`,                     // 01-08
		`\s+\d{1,2}/\d{1,2}\s*$`,                     // 01/08
	}
	for _, pattern := range trailingDates {
		re := regexp.MustCompile(pattern)
		title = re.ReplaceAllString(title, "")
	}
	// Remove ">>" suffix
	title = strings.TrimSuffix(title, ">>")
	// Clean up whitespace
	title = strings.TrimSpace(title)
	return title
}

// isValidNewsTitle filters out navigation items and too-short titles.
func isValidNewsTitle(title string) bool {
	if len(title) < 6 {
		return false
	}
	skip := []string{"首页", "更多", "查看更多", "登录", "注册", "搜索", "English",
		"学院概况", "师资队伍", "人才培养", "党建工作", "学生工作", "招生就业",
		"科学研究", "交流合作", "校友工作", "诚聘英才", "联系我们", "下载专区",
		"学院简介", "现任领导", "组织机构", "教师名录", "本科生", "研究生",
		"通知公告", "学院新闻", "学院动态", "新闻动态", "新闻公告",
		"本科教育", "研究生教育", "实验平台", "系所介绍", "团委工作",
		"查看详情", "详细>>", "详情>>", "更多>>", ">>",
		"院务公开", "联系方式", "学院通知", "唐臣要闻", "基层动态", "学院要闻",
		"诚聘海内外英才", "设计艺术学院", "临床医学",
		"新闻动态\\ NEWS INFORMATION", "新闻动态\\ NEWS",
	}
	// Prefix-based skip patterns
	skipPrefixes := []string{
		"习近平给四所交通大学全体师生",
		"习近平总书记给四所交通大学全体师生",
	}
	for _, s := range skip {
		// These are navigation labels. Matching the whole title avoids
		// discarding legitimate notices such as "本科生转专业...".
		if title == s {
			return false
		}
	}
	for _, p := range skipPrefixes {
		if strings.HasPrefix(title, p) {
			return false
		}
	}
	// Filter out titles that are just section headers
	if strings.HasSuffix(title, ">>") || strings.HasSuffix(title, "》") {
		return false
	}
	return true
}

// isValidNewsURL filters out non-news URLs.
func isValidNewsURL(href string) bool {
	// Skip external domains (use :// prefix for domain boundary matching)
	if strings.Contains(href, "mp.weixin.qq.com") ||
		strings.Contains(href, "://jiuye.swjtu.edu.cn") ||
		strings.Contains(href, "://cas.swjtu.edu.cn") ||
		strings.Contains(href, "://one.swjtu.edu.cn") ||
		strings.Contains(href, "://i.swjtu.edu.cn") {
		return false
	}
	// Skip OA login pages
	if strings.Contains(href, "user/login") ||
		strings.Contains(href, "login.html") ||
		strings.Contains(href, "cas/login") {
		return false
	}
	return true
}

// isExternalURL checks if a URL is from a different domain than the site.
func isExternalURL(href, baseURL string) bool {
	if !strings.HasPrefix(href, "http") {
		return false
	}
	// Extract domain from baseURL
	baseDomain := extractDomain(baseURL)
	hrefDomain := extractDomain(href)
	return baseDomain != hrefDomain && hrefDomain != ""
}

func extractDomain(u string) string {
	// Simple domain extraction
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	if idx := strings.Index(u, "/"); idx > 0 {
		u = u[:idx]
	}
	// Remove port
	if idx := strings.Index(u, ":"); idx > 0 {
		u = u[:idx]
	}
	return u
}

// cleanURL removes trailing %7D and other artifacts.
func cleanURL(u string) string {
	u = strings.TrimRight(u, "%7D")
	u = strings.TrimRight(u, "}")
	return u
}

// isInNavArea checks if a selection is inside a navigation/header/footer area.
func isInNavArea(sel *goquery.Selection) bool {
	for _, navSel := range navSelectors {
		if sel.Closest(navSel).Length() > 0 {
			return true
		}
	}
	return false
}

func parseBodaNewsList(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var items []model.NewsItem
	seen := make(map[string]bool)

	// Pattern 1: Standard list with <li class="flex">
	// <li class="flex"><span class="time">date</span><a href="url">title</a></li>
	doc.Find("li.flex").Each(func(i int, sel *goquery.Selection) {
		if isInNavArea(sel) {
			return
		}
		date := strings.TrimSpace(sel.Find("span.time").Text())
		a := sel.Find("a[href]")
		if a.Length() == 0 {
			return
		}
		title := strings.TrimSpace(a.Text())
		href, _ := a.Attr("href")
		if title == "" || href == "" || !isValidNewsTitle(title) {
			return
		}
		fullURL := cleanURL(resolveURL(baseURL, href))
		if seen[fullURL] || !isValidNewsURL(fullURL) {
			return
		}
		seen[fullURL] = true
		items = append(items, model.NewsItem{
			Title:    title,
			URL:      fullURL,
			Date:     date,
			ParsedAt: time.Now(),
		})
	})

	// Pattern 2: News sections with specific class names.
	if len(items) == 0 {
		doc.Find(".list_news li, .news_cont li, .list_zx li, .list_sd2 li, .xw-ul li, .notice li, .news-list li, .newsList li, ul.xw-ul li, ul.news-list li, .lists li, .list-item, .news-item, .center-news-top, .top-news-item, .item, div.news-list").Each(func(i int, sel *goquery.Selection) {
			if isInNavArea(sel) {
				return
			}
			// sel might be the <a> tag itself (e.g. .list-item) or a <li> containing <a>
			a := sel
			href, _ := sel.Attr("href")
			if href == "" {
				a = sel.Find("a[href]")
				if a.Length() == 0 {
					return
				}
				href, _ = a.Attr("href")
			}
			// Try title from attribute first, then specific class, then link text
			title := strings.TrimSpace(a.AttrOr("title", ""))
			if title == "" {
				// Clone the selection and remove known non-title elements before extracting text
				titleSel := sel.Find(".xw-ul-tt, .line1, .txts, .a_block, .news-title, h3, h4, .info, p.line2").First()
				if titleSel.Length() > 0 {
					// Remove category labels and other noise
					titleSel.Find(".lanmu, .source, .date, .time").Remove()
					title = strings.TrimSpace(titleSel.Text())
				}
			}
			// Try <p> tag inside <a> (ese.swjtu.edu.cn pattern)
			if title == "" {
				pTag := a.Find("p").First()
				if pTag.Length() > 0 {
					title = strings.TrimSpace(pTag.Text())
				}
			}
			if title == "" {
				title = strings.TrimSpace(a.Text())
				if len(title) > 100 {
					title = strings.TrimSpace(sel.Find(".xw-ul-tt, .line1, .txts, .a_block").Text())
				}
			}
			// If still empty, try img title/alt attribute
			if title == "" {
				img := a.Find("img").First()
				if img.Length() > 0 {
					title = strings.TrimSpace(img.AttrOr("title", ""))
					if title == "" {
						title = strings.TrimSpace(img.AttrOr("alt", ""))
					}
				}
			}
			// Clean title: remove date prefixes and other noise
			title = cleanTitle(title)
			if title == "" || href == "" || !isValidNewsTitle(title) {
				return
			}
			if len(title) > 100 || strings.Contains(title, "来源") {
				return
			}
			// Try multiple date selectors
			date := ""
			dateSelectors := []string{".time", ".time span", "span.time", "em", ".date", ".xw-ul-date font", ".xw-ul-date span", ".calendar .date", ".date-label", ".time-box", "time"}
			for _, dateSel := range dateSelectors {
				date = strings.TrimSpace(sel.Find(dateSel).Text())
				if date != "" {
					break
				}
			}
			// Try <time> sibling (sports.swjtu.edu.cn pattern)
			if date == "" {
				parent := sel.Parent()
				if parent.Length() > 0 {
					date = strings.TrimSpace(parent.Find("time").Text())
				}
			}
			// Special handling for ese date format: <div class="s2-date"><b>06-09</b><span>/ 2026</span></div>
			if date == "" {
				s2date := a.Find(".s2-date, .s3-date, .s1-date")
				if s2date.Length() > 0 {
					day := strings.TrimSpace(s2date.Find("b").Text())
					year := strings.TrimSpace(s2date.Find("span").Text())
					year = strings.TrimPrefix(year, "/")
					year = strings.TrimSpace(year)
					if day != "" && year != "" {
						date = year + "-" + day
					}
				}
			}
			// Special handling for ctt date format: <div class="news-time"><div class="day">24</div><div class="month">2025.11</div></div>
			if date == "" {
				day := strings.TrimSpace(sel.Find(".news-time .day, .xw-ul-date span").Text())
				ym := strings.TrimSpace(sel.Find(".news-time .month, .xw-ul-date font").Text())
				if day != "" && ym != "" {
					date = ym + "/" + day
				}
			}
			// Clean up date: remove newlines and extra spaces
			date = strings.Join(strings.Fields(date), " ")
			// Validate date format
			if len(date) > 20 || strings.Count(date, " ") > 3 {
				date = ""
			}
			fullURL := cleanURL(resolveURL(baseURL, href))
			if seen[fullURL] || !isValidNewsURL(fullURL) {
				return
			}
			seen[fullURL] = true
			items = append(items, model.NewsItem{
				Title:    title,
				URL:      fullURL,
				Date:     date,
				ParsedAt: time.Now(),
			})
		})
	}

	// Pattern 3: Links with <a href="info/xxx/xxxx.htm"> inside content areas only.
	// More restrictive than before - must be in a content section and have reasonable title.
	if len(items) == 0 {
		doc.Find("a[href*='info/']").Each(func(i int, sel *goquery.Selection) {
			if isInNavArea(sel) {
				return
			}
			href, _ := sel.Attr("href")
			title := strings.TrimSpace(sel.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(sel.Text())
			}
			// If still empty, try img title/alt attribute
			if title == "" {
				img := sel.Find("img").First()
				if img.Length() > 0 {
					title = strings.TrimSpace(img.AttrOr("title", ""))
					if title == "" {
						title = strings.TrimSpace(img.AttrOr("alt", ""))
					}
				}
			}
			// Clean title: remove date prefixes and other noise
			title = cleanTitle(title)
			if title == "" || href == "" || !strings.Contains(href, ".htm") {
				return
			}
			if !isValidNewsTitle(title) {
				return
			}
			if len(title) > 100 || strings.Contains(title, "来源") || strings.Contains(title, "日期") {
				return
			}
			fullURL := cleanURL(resolveURL(baseURL, href))
			if seen[fullURL] || !isValidNewsURL(fullURL) {
				return
			}
			seen[fullURL] = true

			date := ""
			parent := sel.Parent()
			if parent.Length() > 0 {
				date = strings.TrimSpace(parent.Find(".time, span.time, em, .date").Text())
			}
			// Validate date format
			if len(date) > 20 || strings.Count(date, " ") > 3 {
				date = ""
			}

			items = append(items, model.NewsItem{
				Title:    title,
				URL:      fullURL,
				Date:     date,
				ParsedAt: time.Now(),
			})
		})
	}

	// Pattern 4: Non-博达 CMS sites (e.g. scai.swjtu.edu.cn)
	// Links with page-newsDetail.html?nid=UUID format
	if len(items) == 0 {
		doc.Find("a[href*='newsDetail']").Each(func(i int, sel *goquery.Selection) {
			if isInNavArea(sel) {
				return
			}
			href, _ := sel.Attr("href")
			title := strings.TrimSpace(sel.AttrOr("title", ""))
			if title == "" {
				title = strings.TrimSpace(sel.Text())
			}
			if title == "" || href == "" {
				return
			}
			if !isValidNewsTitle(title) || len(title) > 100 {
				return
			}
			fullURL := cleanURL(resolveURL(baseURL, href))
			if seen[fullURL] || !isValidNewsURL(fullURL) {
				return
			}
			seen[fullURL] = true

			// Try to find date from sibling/parent
			date := ""
			parent := sel.Parent()
			if parent.Length() > 0 {
				date = strings.TrimSpace(parent.Find(".date-label, .date, .calendar .date, span[class*='date']").Text())
				if date == "" {
					// Check for calendar pattern: <p class="day">12</p><p class="date">2026-06</p>
					day := strings.TrimSpace(parent.Find(".calendar .day, .day").Text())
					yearMonth := strings.TrimSpace(parent.Find(".calendar .date, .date").Text())
					if day != "" && yearMonth != "" {
						date = yearMonth + "-" + day
					}
				}
			}
			// Validate date format
			if len(date) > 20 || strings.Count(date, " ") > 3 {
				date = ""
			}

			items = append(items, model.NewsItem{
				Title:    title,
				URL:      fullURL,
				Date:     date,
				ParsedAt: time.Now(),
			})
		})
	}

	// Pattern 5: design.swjtu.edu.cn style - news in <a> tags with title attr inside content divs
	if len(items) == 0 {
		doc.Find("a[title][href*='.htm']").Each(func(i int, sel *goquery.Selection) {
			if isInNavArea(sel) {
				return
			}
			href, _ := sel.Attr("href")
			title := strings.TrimSpace(sel.AttrOr("title", ""))
			if title == "" || href == "" {
				return
			}
			if !isValidNewsTitle(title) || len(title) > 100 {
				return
			}
			fullURL := cleanURL(resolveURL(baseURL, href))
			if seen[fullURL] || !isValidNewsURL(fullURL) {
				return
			}
			seen[fullURL] = true

			date := ""
			parent := sel.Parent()
			if parent.Length() > 0 {
				date = strings.TrimSpace(parent.Find(".time, .date, em, span[class*='time']").Text())
			}
			// Validate date format
			if len(date) > 20 || strings.Count(date, " ") > 3 {
				date = ""
			}

			items = append(items, model.NewsItem{
				Title:    title,
				URL:      fullURL,
				Date:     date,
				ParsedAt: time.Now(),
			})
		})
	}

	return items, nil
}

func parseBodaArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	art := &model.Article{}

	// Remember which element the headline came from: the meta line (date,
	// source) usually sits in its immediate neighborhood, and confining the
	// generic date search to that zone avoids picking up event dates from
	// the article body (时间：2026年9月16日 in a notice, etc.).
	var titleSel *goquery.Selection
	if sel := doc.Find("h1.news_tit").First(); sel.Length() > 0 {
		art.Title = strings.TrimSpace(sel.Text())
		titleSel = sel
	}
	if art.Title == "" {
		if sel := doc.Find(".news-title").First(); sel.Length() > 0 {
			art.Title = strings.TrimSpace(sel.Text())
			titleSel = sel
		}
	}
	if art.Title == "" {
		if sel := doc.Find("h1").First(); sel.Length() > 0 {
			art.Title = strings.TrimSpace(sel.Text())
			titleSel = sel
		}
	}
	// Newer 博达 templates (news.swjtu.edu.cn and many unit sites) put the
	// headline in .art-tit/.cont-tit instead of an h1.
	if art.Title == "" {
		for _, sel := range []string{".art-tit h1", ".art-tit h2", ".art-tit h3", ".cont-tit h1", ".cont-tit h2", ".cont-tit h3", ".article-title", ".arti-title", ".detail-title"} {
			if found := doc.Find(sel).First(); found.Length() > 0 {
				if title := strings.TrimSpace(found.Text()); title != "" {
					art.Title = title
					titleSel = found
					break
				}
			}
		}
	}
	// 博达 pages almost always carry a clean pageTitle meta.
	if art.Title == "" {
		art.Title = strings.TrimSpace(doc.Find(`meta[name="pageTitle"]`).AttrOr("content", ""))
	}
	if art.Title == "" {
		art.Title = stripSiteSuffix(strings.TrimSpace(doc.Find("title").First().Text()))
	}

	// Source + date
	sourceBlock := doc.Find(".news_source")
	sourceBlock.Find("span").Each(func(i int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		if strings.HasPrefix(text, "来源：") || strings.HasPrefix(text, "来源:") {
			art.Source = strings.TrimPrefix(strings.TrimPrefix(text, "来源："), "来源:")
		}
		if strings.HasPrefix(text, "日期：") || strings.HasPrefix(text, "日期:") {
			art.Date = strings.TrimPrefix(strings.TrimPrefix(text, "日期："), "日期:")
		}
		if sel.HasClass("date") {
			art.Date = strings.TrimSpace(sel.Text())
		}
	})

	// Fallback: top-info pattern (civil.swjtu.edu.cn etc.)
	if art.Date == "" {
		topInfo := doc.Find(".top-info")
		if topInfo.Length() > 0 {
			topInfo.Find("span").Each(func(i int, sel *goquery.Selection) {
				text := strings.TrimSpace(sel.Text())
				// Check for date pattern YYYY-MM-DD
				if art.Date == "" && len(text) >= 10 && text[4] == '-' && text[7] == '-' {
					art.Date = text[:10]
				}
				if strings.HasPrefix(text, "来源：") || strings.HasPrefix(text, "来源:") {
					art.Source = strings.TrimPrefix(strings.TrimPrefix(text, "来源："), "来源:")
				}
			})
		}
	}

	// bksy.swjtu.edu.cn uses .tips instead of the older .news_source block.
	if tips := doc.Find(".tips"); tips.Length() > 0 {
		if art.Date == "" {
			dateText := strings.TrimSpace(tips.Find(".left-details").First().Text())
			if match := regexp.MustCompile(`\d{4}[-/.]\d{1,2}[-/.]\d{1,2}`).FindString(dateText); match != "" {
				art.Date = strings.ReplaceAll(strings.ReplaceAll(match, "/", "-"), ".", "-")
			}
		}
		if art.Source == "" {
			source := strings.TrimSpace(tips.Find(".middle2").First().Text())
			source = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(source, "来源："), "来源:"))
			art.Source = source
		}
	}

	// Content body
	contentSel := doc.Find("#vsb_content_501 .v_news_content")
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".v_news_content")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find("#vsb_content_501")
	}
	// scai.swjtu.edu.cn and other newer templates use their own wrappers.
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".news-content-wrapper")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find("[id^='vsb_content']")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".content-main")
	}
	// Some templates render the article twice (desktop + mobile copies);
	// keep only the first so text isn't duplicated and zone exclusion works.
	contentSel = contentSel.First()

	// Generic labeled-text fallback: many unit templates render the meta line
	// as plain text (日期：2026/08/21, 信息来源：xxx, 作者：xxx) without any
	// of the containers handled above. Runs after contentSel is known so the
	// date search can be confined to the meta zone around the article.
	fillGenericMeta(doc, art, titleSel, contentSel)
	if contentSel.Length() > 0 {
		var parts []string
		contentSel.Find("p, h1, h2, h3, h4").Each(func(i int, sel *goquery.Selection) {
			text := strings.TrimSpace(sel.Text())
			if text != "" {
				parts = append(parts, text)
			}
		})
		art.Content = strings.Join(parts, "\n\n")

		contentSel.Find("img").Each(func(i int, sel *goquery.Selection) {
			src, _ := sel.Attr("src")
			if src != "" {
				resolved := resolveURL(baseURL, src)
				art.Images = append(art.Images, resolved)
				sel.SetAttr("src", resolved)
			}
		})

		// Extract attachments
		// Pattern 1: Links with file extensions in href
		// Pattern 2: Links with "download" attribute (博达CMS stores .docx/.xlsx as .png internally)
		attachExts := []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".pdf", ".zip", ".rar", ".7z", ".txt", ".csv"}
		contentSel.Find("a[href]").Each(func(i int, sel *goquery.Selection) {
			href, _ := sel.Attr("href")
			if href == "" {
				return
			}
			hrefLower := strings.ToLower(href)
			downloadName, hasDownload := sel.Attr("download")
			nameAttr, hasName := sel.Attr("name")
			linkText := strings.TrimSpace(sel.Text())

			// Determine if this is an attachment
			isAttach := false
			attName := ""

			// Check download attribute first (博达CMS pattern)
			if hasDownload && downloadName != "" {
				isAttach = true
				attName = downloadName
			} else if hasName {
				for _, ext := range attachExts {
					if strings.HasSuffix(strings.ToLower(nameAttr), ext) {
						isAttach = true
						attName = nameAttr
						break
					}
				}
			}

			// Check href for file extensions
			if !isAttach {
				for _, ext := range attachExts {
					if strings.Contains(hrefLower, ext) {
						isAttach = true
						if linkText != "" {
							attName = linkText
						} else {
							attName = href
						}
						break
					}
				}
			}

			if isAttach && attName != "" {
				resolved := resolveURL(baseURL, href)
				art.Attachments = append(art.Attachments, model.Attachment{
					Name: attName,
					URL:  resolved,
				})
				sel.SetAttr("href", resolved)
			}
		})
		if html, htmlErr := contentSel.Html(); htmlErr == nil {
			art.ContentHTML = html
		}
	}

	// Some Visual SiteBuilder templates render the attachment list next to,
	// rather than inside, .v_news_content (bksy.swjtu.edu.cn is one example).
	attachmentURLs := make(map[string]bool, len(art.Attachments))
	for _, attachment := range art.Attachments {
		attachmentURLs[attachment.URL] = true
	}
	doc.Find(".left-news-details li a[href]").Each(func(_ int, sel *goquery.Selection) {
		parentText := strings.TrimSpace(sel.Parent().Text())
		if !strings.Contains(parentText, "附件") {
			return
		}
		href, _ := sel.Attr("href")
		name := strings.TrimSpace(sel.Text())
		if href == "" || name == "" {
			return
		}
		resolved := resolveURL(baseURL, href)
		if attachmentURLs[resolved] {
			return
		}
		attachmentURLs[resolved] = true
		art.Attachments = append(art.Attachments, model.Attachment{Name: name, URL: resolved})
	})

	authorBlock := doc.Find(".xxx")
	if authorBlock.Length() > 0 {
		authorText := authorBlock.Text()
		for _, part := range strings.Split(authorText, " ") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "作者：") {
				art.Author = strings.TrimPrefix(part, "作者：")
			}
			if strings.HasPrefix(part, "摄影：") {
				art.Photographer = strings.TrimPrefix(part, "摄影：")
			}
			if strings.HasPrefix(part, "编辑：") {
				art.Editor = strings.TrimPrefix(part, "编辑：")
			}
		}
	}

	doc.Find(".news_sxp li a").Each(func(i int, sel *goquery.Selection) {
		text := strings.TrimSpace(sel.Text())
		href, _ := sel.Attr("href")
		if href == "" {
			return
		}
		fullURL := resolveURL(baseURL, href)
		if strings.HasPrefix(text, "上一篇") {
			art.PrevURL = fullURL
			art.PrevTitle = text
		} else if strings.HasPrefix(text, "下一篇") {
			art.NextURL = fullURL
			art.NextTitle = text
		}
	})

	return art, nil
}

var (
	labelDateRe         = regexp.MustCompile(`(?:^|[\s\x{a0}：:，。；;、|])(?:日期|发布时间|发布日期|发表日期|发表时间|发布于|时间)[\s\x{a0}]*[:：][\s\x{a0}]*(20\d{2})[\s\x{a0}]*[-/.年][\s\x{a0}]*(\d{1,2})[\s\x{a0}]*[-/.月][\s\x{a0}]*(\d{1,2})`)
	javaDateRe          = regexp.MustCompile(`(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{2} \d{2}:\d{2}:\d{2} CST \d{4}`)
	bareDateRe          = regexp.MustCompile(`(20\d{2})\s*[-/.]\s*(\d{1,2})\s*[-/.]\s*(\d{1,2})`)
	labelSourceRe       = regexp.MustCompile(`(?:信息来源|来源)\s*[:：][\s\x{a0}]*([^\s\x{a0}，。；;:：<>（）()]{2,40})`)
	labelAuthorRe       = regexp.MustCompile(`作者\s*[:：][\s\x{a0}]*([^\s\x{a0}，。；;:：<>（）()]{2,30})`)
	labelEditorRe       = regexp.MustCompile(`(?:编辑|责编)\s*[:：][\s\x{a0}]*([^\s\x{a0}，。；;:：<>（）()]{2,30})`)
	labelPhotographerRe = regexp.MustCompile(`摄影\s*[:：][\s\x{a0}]*([^\s\x{a0}，。；;:：<>（）()]{2,30})`)
	siteSuffixRe        = regexp.MustCompile(`[-–—_][^-–—_]{0,60}$`)
	siteWordsRe         = regexp.MustCompile(`大学|学院|研究院|新闻网|党委|宣传|财务|审计|工会|处|部|中心|图书馆|档案馆|医院|集团|实验室|管委会|办公室|期刊社`)
)

// labelWords are tokens that appear as field labels on article pages. When a
// captured value is one of these, the label was empty and the regex consumed
// the next label instead (e.g. "来源：作者：张三" in flattened text).
var labelWords = map[string]bool{
	"来源": true, "信息来源": true, "作者": true, "编辑": true, "责编": true,
	"摄影": true, "日期": true, "时间": true, "审核": true, "点击": true,
	"点击量": true, "浏览": true, "发布": true, "发布日期": true, "发布时间": true,
	"发表时间": true, "发布者": true, "供稿": true, "撰稿": true, "文字": true, "图片": true,
}

var labelValueNoise = regexp.MustCompile(`(20\d{2}年.*|https?$|^\d)`)

var labelCutRe = regexp.MustCompile(`(日期|时间|来源|编辑|责编|审核|点击|浏览|摄影)`)

func cleanLabelValue(value string) string {
	value = strings.TrimSpace(value)
	if cut := labelCutRe.FindStringIndex(value); cut != nil {
		value = value[:cut[0]]
	}
	if labelWords[value] || labelValueNoise.MatchString(value) {
		return ""
	}
	return value
}

// stripSiteSuffix removes a trailing "- 西南交通大学xx学院" style suffix from
// a <title> tag while leaving dashes inside the headline itself untouched.
func stripSiteSuffix(title string) string {
	suffix := siteSuffixRe.FindString(title)
	if suffix != "" && siteWordsRe.MatchString(suffix) {
		return strings.TrimSpace(strings.TrimSuffix(title, suffix))
	}
	return title
}

func normalizeDateParts(year, month, day string) string {
	m, errM := strconv.Atoi(month)
	d, errD := strconv.Atoi(day)
	if errM != nil || errD != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return ""
	}
	return fmt.Sprintf("%s-%02d-%02d", year, m, d)
}

// fillGenericMeta extracts date/source/author fields using the Chinese
// labels (日期：, 信息来源：, 作者：…) that most unit templates render
// without any stable CSS container. Fields that were already filled by the
// site-specific selectors above are kept.
//
// The date search is deliberately confined to the "meta zone" — the
// neighborhood of the headline and the content container — because article
// bodies are full of event dates (时间：2026年9月16日 in a notice) that must
// never be mistaken for the publish date.
func fillGenericMeta(doc *goquery.Document, art *model.Article, titleSel, contentSel *goquery.Selection) {
	text := ""
	visibleText := func() string {
		if text == "" {
			clone := doc.Clone()
			clone.Find("script, style, noscript").Remove()
			text = clone.Text()
		}
		return text
	}
	if art.Date == "" {
		art.Date = findDateInMetaZone(titleSel, contentSel)
	}
	if art.Date == "" {
		for _, sel := range []string{`meta[name="publishdate"]`, `meta[name="publish_date"]`, `meta[name="PubDate"]`, `meta[property="article:published_time"]`} {
			content := strings.TrimSpace(doc.Find(sel).AttrOr("content", ""))
			if m := bareDateRe.FindStringSubmatch(content); m != nil {
				if date := normalizeDateParts(m[1], m[2], m[3]); date != "" {
					art.Date = date
					break
				}
			}
		}
	}
	// scai.swjtu.edu.cn renders "发布日期：Tue Jul 07 00:00:00 CST 2026";
	// this format is distinctive enough to match against the whole page.
	if art.Date == "" {
		if m := javaDateRe.FindString(visibleText()); m != "" {
			if parsed, err := time.Parse("Mon Jan 02 15:04:05 MST 2006", m); err == nil {
				art.Date = parsed.Format("2006-01-02")
			}
		}
	}
	if art.Source == "" {
		if m := labelSourceRe.FindStringSubmatch(visibleText()); m != nil {
			art.Source = cleanLabelValue(m[1])
		}
	}
	if art.Author == "" {
		if m := labelAuthorRe.FindStringSubmatch(visibleText()); m != nil {
			art.Author = cleanLabelValue(m[1])
		}
	}
	if art.Editor == "" {
		if m := labelEditorRe.FindStringSubmatch(visibleText()); m != nil {
			art.Editor = cleanLabelValue(m[1])
		}
	}
	if art.Photographer == "" {
		if m := labelPhotographerRe.FindStringSubmatch(visibleText()); m != nil {
			art.Photographer = cleanLabelValue(m[1])
		}
	}
}

var (
	scriptBlockRe  = regexp.MustCompile(`(?is)<script[\s\S]*?</script>|<style[\s\S]*?</style>|<noscript[\s\S]*?</noscript>`)
	tagStripRe     = regexp.MustCompile(`<[^>]*>`)
	spaceCollapseRe = regexp.MustCompile(`[\s\x{a0}]+`)
)

// flattenedText renders a selection as plain text with a space wherever a
// tag boundary was, so label regexes can tell "时间：" (a label) apart from
// "截止时间：" (a word inside a sentence). Selection.Text() glues sibling
// elements together and loses that distinction.
func flattenedText(sel *goquery.Selection) string {
	inner, err := sel.Html()
	if err != nil {
		return sel.Text()
	}
	text := scriptBlockRe.ReplaceAllString(inner, " ")
	text = tagStripRe.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	return spaceCollapseRe.ReplaceAllString(text, " ")
}

// findDateInMetaZone looks for a publish date in the ancestors of the
// headline and of the content container (with the article body itself
// excluded), so dates mentioned inside the article text are never picked.
func findDateInMetaZone(titleSel, contentSel *goquery.Selection) string {
	zones := make([]string, 0, 8)
	contentText := ""
	collect := func(sel *goquery.Selection, exclude string) {
		count := 0
		sel.Parents().EachWithBreak(func(_ int, parent *goquery.Selection) bool {
			if count >= 4 {
				return false
			}
			count++
			text := flattenedText(parent)
			if exclude != "" {
				text = strings.Replace(text, exclude, "", 1)
			}
			zones = append(zones, text)
			return true
		})
	}
	if contentSel != nil && contentSel.Length() > 0 {
		contentText = flattenedText(contentSel)
		collect(contentSel, contentText)
	}
	if titleSel != nil && titleSel.Length() > 0 {
		collect(titleSel, "")
	}
	// Labeled dates first: they are the strongest signal even in wider zones.
	for _, zone := range zones {
		if m := labelDateRe.FindStringSubmatch(zone); m != nil {
			if date := normalizeDateParts(m[1], m[2], m[3]); date != "" {
				return date
			}
		}
	}
	// Then bare dates, but only in the tightest zones (the meta line sits
	// right next to the headline; wider zones contain related-news dates).
	for _, zone := range zones[:min(2, len(zones))] {
		if m := bareDateRe.FindStringSubmatch(zone); m != nil {
			if date := normalizeDateParts(m[1], m[2], m[3]); date != "" {
				return date
			}
		}
	}
	return ""
}

func resolveURL(base, href string) string {
	href = cleanURL(href)
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	u, err := url.Parse(base)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return u.ResolveReference(ref).String()
}

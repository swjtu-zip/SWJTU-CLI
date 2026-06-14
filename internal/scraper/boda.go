package scraper

import (
	"fmt"
	"net/url"
	"regexp"
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
	var listURL string
	if page <= 1 {
		listURL = fmt.Sprintf("%s/%s.htm", s.BaseURL, slug)
	} else {
		// Need to discover total pages first for page 2+
		// For now, try the direct page number format
		listURL = fmt.Sprintf("%s/%s/%d.htm", s.BaseURL, slug, page)
	}

	body, err := fetch(listURL)
	if err != nil {
		// Try .html extension
		if page <= 1 {
			listURL = fmt.Sprintf("%s/%s.html", s.BaseURL, slug)
		} else {
			listURL = fmt.Sprintf("%s/%s/%d.html", s.BaseURL, slug, page)
		}
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
	listURL := fmt.Sprintf("%s/%s.htm", s.BaseURL, slug)
	body, err := fetch(listURL)
	if err != nil {
		listURL = fmt.Sprintf("%s/%s.html", s.BaseURL, slug)
		body, err = fetch(listURL)
		if err != nil {
			return 0, err
		}
	}
	return parseTotalPages(body), nil
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
			// Need more pages - fetch in descending page order
			// Page 2 = totalPages, page 3 = totalPages-1, etc.
			for p := 2; p <= totalPages; p++ {
				realPage := totalPages + 2 - p
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
					if !t.IsZero() && t.After(toTime.Add(24 * time.Hour)) {
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
	body, err := fetch(articleURL)
	if err != nil {
		return nil, err
	}
	return parseBodaArticle(body, s.BaseURL)
}

// navSelectors are CSS selectors for non-content areas to exclude.
var navSelectors = []string{
	"header", "nav", "footer", ".header", ".nav", ".footer",
	".menu", ".top", ".link", ".share", ".search", ".crumbs",
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
		`\s+\d{1,2}-\d{1,2}\s*$`,                          // 01-08
		`\s+\d{1,2}/\d{1,2}\s*$`,                          // 01/08
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
		if title == s || strings.Contains(title, s) {
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
	// Skip external domains
	if strings.Contains(href, "mp.weixin.qq.com") ||
		strings.Contains(href, "jiuye.swjtu.edu.cn") ||
		strings.Contains(href, "cas.swjtu.edu.cn") ||
		strings.Contains(href, "one.swjtu.edu.cn") ||
		strings.Contains(href, "i.swjtu.edu.cn") {
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

	art.Title = strings.TrimSpace(doc.Find("h1.news_tit").Text())
	if art.Title == "" {
		art.Title = strings.TrimSpace(doc.Find("h1").First().Text())
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

	// Content body
	contentSel := doc.Find("#vsb_content_501 .v_news_content")
	if contentSel.Length() == 0 {
		contentSel = doc.Find(".v_news_content")
	}
	if contentSel.Length() == 0 {
		contentSel = doc.Find("#vsb_content_501")
	}
	if contentSel.Length() > 0 {
		var parts []string
		contentSel.Find("p").Each(func(i int, sel *goquery.Selection) {
			text := strings.TrimSpace(sel.Text())
			if text != "" {
				parts = append(parts, text)
			}
		})
		art.Content = strings.Join(parts, "\n\n")

		contentSel.Find("img").Each(func(i int, sel *goquery.Selection) {
			src, _ := sel.Attr("src")
			if src != "" {
				art.Images = append(art.Images, resolveURL(baseURL, src))
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
				art.Attachments = append(art.Attachments, model.Attachment{
					Name: attName,
					URL:  resolveURL(baseURL, href),
				})
			}
		})
	}

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

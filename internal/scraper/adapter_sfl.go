package scraper

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

// sfl.swjtu.edu.cn has a legacy 博达 template whose home page is an
// aggregation of several sections. The home-page links do not carry dates;
// the canonical学院新闻 list (xyxw.htm) does. Keep the two shapes separate
// so image cards and navigation links cannot become news items.
func init() {
	registerAdapter("sfl", SiteAdapter{
		SubPages:     sflSubPages,
		ParseList:    parseSflNews,
		ParseArticle: parseSflArticle,
		PageURL:      sflPageURL,
		TotalPages:   sflTotalPages,
		FetchArticle: sflFetchArticle,
	})
}

// These are the article-bearing pages linked from the live navigation, plus
// the seven news and seven notice tabs that the home page exposes only by
// JavaScript hover handlers. Static profile/intro pages are deliberately not
// registered as feeds.
var sflSubPages = []string{
	"xyxw",
	"xyxw/dzbg",
	"xyxw/bksjy",
	"xyxw/yjsjy",
	"xyxw/xsgz",
	"xyxw/gjjl",
	"xyxw/xsyj",
	"tzgg",
	"tzgg/dzbg",
	"tzgg/bksjy",
	"tzgg/yjsjy",
	"tzgg/xsgz",
	"tzgg/gjjl",
	"tzgg/xsyj",
	"zgzj1/xwgg",
	"yyx/xwgg",
	"gjhyx/xwgg",
	"ryx/xwgg",
	"swyyx/xwgg",
	"dyx/xwgg",
	"fex/xwgg",
	"fyx/xwgg",
	"bkjy/xwgg",
	"ryx1/xwgg",
	"ryx1/sbzs",
	"xsky/xwgg",
	"xsky/kyxm",
	"xsky/kylw",
	"xsky/zzyz",
	"xsky/tbtj",
	"xsky/zlxz",
	"xsky/mgzx/xwgg",
	"xsky/mgzx/gzjb",
	"xsky/mgzx/xmgl",
	"xsky/mgzx/zxkw",
	"xsky/mgzx/zzjg",
	"xsky/ozzx/xwgg",
	"xsky/ozzx/gjjl",
	"xsky/ozzx/jxkc",
	"xsky/ozzx/jzjs",
	"xsky/ozzx/r_mnxm",
	"xsky/ozzx/tbtj",
	"xsky/ozzx/xshd",
	"gjjl/xwgg",
	"gjjl/dwhy/kcap",
	"gjjl/dwhy/szjs",
	"gjjl/dwhy/zsjz",
	"gjjl/hzxm",
	"gjjl/wjsz",
	"djgz/djdt",
	"djgz/zlhb",
	"xsgz/xgdt",
	"xsgz/jyxx",
	"xsgz/jdyd",
	"xsgz/zlxz",
	"xygz/xygz",
	"zgzj/ghdt",
	"zgzj/txgz_ggwgz_",
	"zgzj/jdh",
	"zsjy",
	"zsjy/bkzs/kcjz",
	"zsjy/jygz",
	"zsjy/yjszs",
}

var sflFeedTypes = map[string]string{
	"xyxw":             "学院新闻",
	"xyxw/dzbg":        "党政办公",
	"xyxw/bksjy":       "本科生教育",
	"xyxw/yjsjy":       "研究生教育",
	"xyxw/xsgz":        "学生工作",
	"xyxw/gjjl":        "国际交流",
	"xyxw/xsyj":        "学术研究",
	"tzgg":             "通知公告",
	"tzgg/dzbg":        "党政办公",
	"tzgg/bksjy":       "本科生教育",
	"tzgg/yjsjy":       "研究生教育",
	"tzgg/xsgz":        "学生工作",
	"tzgg/gjjl":        "国际交流",
	"tzgg/xsyj":        "学术研究",
	"zgzj1/xwgg":       "工程英语系",
	"yyx/xwgg":         "英语系",
	"gjhyx/xwgg":       "国际汉语系",
	"ryx/xwgg":         "日语系",
	"swyyx/xwgg":       "商务英语系",
	"dyx/xwgg":         "德语系",
	"fex/xwgg":         "法俄系",
	"fyx/xwgg":         "翻译系",
	"bkjy/xwgg":        "本科生教育",
	"ryx1/xwgg":        "研究生教育",
	"ryx1/sbzs":        "硕博招生",
	"xsky/xwgg":        "学术科研",
	"xsky/kyxm":        "科研项目",
	"xsky/kylw":        "科研论文",
	"xsky/zzyz":        "专著译著",
	"xsky/tbtj":        "特别推荐",
	"xsky/zlxz":        "资料下载",
	"xsky/mgzx/xwgg":   "美国中心",
	"xsky/mgzx/gzjb":   "工作简报",
	"xsky/mgzx/xmgl":   "项目管理",
	"xsky/mgzx/zxkw":   "中心刊物",
	"xsky/mgzx/zzjg":   "组织机构",
	"xsky/ozzx/xwgg":   "欧洲中心",
	"xsky/ozzx/gjjl":   "国际交流",
	"xsky/ozzx/jxkc":   "教学课程",
	"xsky/ozzx/jzjs":   "讲座教授",
	"xsky/ozzx/r_mnxm": "让•莫内项目",
	"xsky/ozzx/tbtj":   "特别推荐",
	"xsky/ozzx/xshd":   "学术活动",
	"gjjl/xwgg":        "国际交流",
	"gjjl/dwhy/kcap":   "课程安排",
	"gjjl/dwhy/szjs":   "师资介绍",
	"gjjl/dwhy/zsjz":   "招生简章",
	"gjjl/hzxm":        "合作项目",
	"gjjl/wjsz":        "外教师资",
	"djgz/djdt":        "党建动态",
	"djgz/zlhb":        "资料汇编",
	"xsgz/xgdt":        "学工动态",
	"xsgz/jyxx":        "就业信息",
	"xsgz/jdyd":        "经典悦读",
	"xsgz/zlxz":        "资料下载",
	"xygz/xygz":        "校友工作",
	"zgzj/ghdt":        "工会动态",
	"zgzj/txgz_ggwgz_": "退休工作(关工委工作)",
	"zgzj/jdh":         "教代会",
	"zsjy":             "外国语学院",
	"zsjy/bkzs/kcjz":   "课程矩阵",
	"zsjy/jygz":        "就业工作",
	"zsjy/yjszs":       "研究生招生",
}

var (
	sflArticlePathRe     = regexp.MustCompile(`(?i)^/info/(\d+)/(\d+)\.html?$`)
	sflDateRe            = regexp.MustCompile(`(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})\s*日?`)
	sflNoticeDateRe      = regexp.MustCompile(`^\s*(\d{1,2})\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})`)
	sflChineseDateTimeRe = regexp.MustCompile(`(20\d{2})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*日?\s*(\d{1,2})\s*(?:时|点|:)\s*(\d{2})\s*(?:分)?(?:\s*(\d{1,2})\s*秒?)?`)
	sflPageInfoRe        = regexp.MustCompile(`共\s*(\d+)\s*条.*?(\d+)\s*/\s*(\d+)`)
	sflLabeledDateRe     = regexp.MustCompile(`(?:发布时间|发布日期|发表日期|发表时间|日期)\s*[:：]\s*(20\d{2})\s*[-/.年]\s*(\d{1,2})\s*[-/.月]\s*(\d{1,2})`)
	sflBlockedTextRe     = regexp.MustCompile(`(?i)安全验证|验证码|access\s+denied|forbidden|waf|请先登录|用户登录|统一身份认证|human verification`)
)

var sflHomeNewsTypes = []string{"学院新闻", "党政办公", "本科生教育", "研究生教育", "学生工作", "国际交流", "学术研究"}
var sflHomeNoticeTypes = []string{"通知公告", "党政办公", "本科生教育", "研究生教育", "学生工作", "国际交流", "学术研究"}

var sflChannelTypes = map[string]string{
	"1043": "工程英语系", "1051": "英语系", "1346": "国际汉语系", "1122": "日语系",
	"1075": "商务英语系", "1083": "德语系", "1088": "法俄系", "1098": "翻译系",
	"1436": "本科生教育", "1218": "研究生教育", "1146": "学术科研", "1149": "科研项目",
	"1150": "科研论文", "1151": "专著译著", "1154": "特别推荐", "1155": "资料下载",
	"1156": "美国中心", "1165": "欧洲中心", "1059": "国际交流", "1109": "招生简章",
	"1062": "合作项目", "1063": "外教师资", "1172": "党建动态", "1174": "资料汇编",
	"1176": "学工动态", "1475": "就业信息", "1182": "经典悦读", "1183": "资料下载",
	"1188": "校友工作", "1198": "教代会", "1199": "工会动态", "1196": "退休工作(关工委工作)",
	"1270": "本科生教育", "1271": "研究生教育", "1272": "学生工作", "1273": "国际交流",
	"1274": "学术研究", "1228": "硕博招生", "1550": "外国语学院", "1570": "就业工作",
}

var sflState = struct {
	sync.RWMutex
	dates       map[string]string
	publishedAt map[string]string
	pageCounts  map[string]int
}{
	dates:       make(map[string]string),
	publishedAt: make(map[string]string),
	pageCounts:  make(map[string]int),
}

func sflTargetSlug(slug string) string {
	slug = strings.Trim(slug, "/")
	if slug == "" || slug == "index" {
		return "xyxw"
	}
	return slug
}

func sflKnownSlug(slug string) bool {
	return sflFeedTypes[sflTargetSlug(slug)] != ""
}

// sflPageURL points the synthetic index feed at the canonical学院新闻 list.
// That list is the only home-level source carrying authoritative dates.
func sflPageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = sflTargetSlug(slug)
	if baseURL == "" || page < 1 || !sflKnownSlug(slug) {
		return ""
	}
	if extension == "" {
		extension = ".htm"
	} else if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if page == 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}

	pageCount := totalPages
	sflState.RLock()
	if known, ok := sflState.pageCounts[slug]; ok {
		pageCount = known
	}
	sflState.RUnlock()
	if pageCount <= 1 || page > pageCount {
		return ""
	}
	pageNumber := pageCount - page + 1
	if pageNumber < 1 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, pageNumber, extension)
}

func parseSflNews(html []byte, baseURL string) ([]model.NewsItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse sfl list: %w", err)
	}

	items := make([]model.NewsItem, 0)
	seen := make(map[string]int)

	// Category/list pages use this exact list. Return immediately when the
	// container exists, even if it is empty: do not fall through to scanning
	// unrelated home-page links on a broken list response.
	list := doc.Find("ul.list-group.rightnew").First()
	if list.Length() > 0 {
		typ := sflTypeFromDocument(doc)
		anchorCount := list.Find("li > a[href]").Length()
		list.Find("li > a[href]").Each(func(_ int, anchor *goquery.Selection) {
			item := anchor.Closest("li")
			addSflItem(&items, seen, anchor, sflRawDateFromListItem(item), typ, baseURL)
		})
		if anchorCount > 0 && len(items) == 0 {
			return nil, fmt.Errorf("sfl list contains no valid local article rows")
		}
		if anchorCount == 0 {
			if _, _, _, ok := sflPageInfoFromDocument(doc); !ok {
				return nil, fmt.Errorf("sfl empty list has no valid pagination marker")
			}
		}
		return items, nil
	}

	// The home page has seven .product blocks. Their .news-text lists are
	// curated article lists; the adjacent .news-img cards are duplicates or
	// historical highlights and do not have reliable dates. Restricting the
	// selector to news-text fixes the old parser's document-wide info/ scan.
	homeNewsRows := doc.Find(".product-wrap .product .news-text ul li > a[href]")
	homeNoticeRows := doc.Find(".product-wrap2 .product .notice .con dl")
	homeShape := doc.Find(".title-list, .title-list2").Length() > 0 &&
		(doc.Find(".product-wrap .product .news-text").Length() > 0 || doc.Find(".product-wrap2 .product .notice").Length() > 0)

	doc.Find(".product-wrap .product").Each(func(productIndex int, product *goquery.Selection) {
		typ := sflHomeType(sflHomeNewsTypes, productIndex)
		product.Find(".news-text ul li > a[href]").Each(func(_ int, anchor *goquery.Selection) {
			addSflItem(&items, seen, anchor, "", typ, baseURL)
		})
	})

	// The notice half of the home page uses a split date: <dt>24<em>2026.06
	// </em></dt>. It is still a real article list, so retain it with a
	// normalized date.
	doc.Find(".product-wrap2 .product").Each(func(productIndex int, product *goquery.Selection) {
		typ := sflHomeType(sflHomeNoticeTypes, productIndex)
		product.Find(".notice .con dl").Each(func(_ int, entry *goquery.Selection) {
			anchor := entry.Find("dd > a[href]").First()
			if anchor.Length() == 0 {
				return
			}
			addSflItem(&items, seen, anchor, sflRawDateFromNotice(entry), typ, baseURL)
		})
	})

	if homeNewsRows.Length()+homeNoticeRows.Length() > 0 {
		if len(items) == 0 {
			return nil, fmt.Errorf("sfl homepage contains no valid local article rows")
		}
		return items, nil
	}
	if homeShape {
		return items, nil
	}
	if sflBlockedTextRe.MatchString(strings.ToLower(string(html))) {
		return nil, fmt.Errorf("sfl response appears to be a blocked or login page")
	}
	return nil, fmt.Errorf("sfl response contains no recognisable article list")
}

func addSflItem(items *[]model.NewsItem, seen map[string]int, anchor *goquery.Selection, date, typ, baseURL string) bool {
	if anchor == nil || anchor.Length() == 0 || isInNavArea(anchor) {
		return false
	}
	href := strings.TrimSpace(anchor.AttrOr("href", ""))
	fullURL, ok := sflArticleURL(baseURL, href)
	if !ok {
		return false
	}

	title := strings.TrimSpace(anchor.AttrOr("title", ""))
	if title == "" {
		title = strings.TrimSpace(anchor.Text())
	}
	title = cleanTitle(strings.Join(strings.Fields(title), " "))
	if title == "" || !isValidNewsTitle(title) {
		return false
	}

	rawDate := date
	date = sflDateFromText(rawDate)
	publishedAt := sflPublishedAtFromText(rawDate)
	if date == "" {
		date = sflCachedDate(fullURL)
	}
	if publishedAt == "" {
		publishedAt = sflCachedPublishedAt(fullURL)
	}
	if publishedAt == "" {
		publishedAt = normalizePublishedAt(date)
	}
	if index, exists := seen[fullURL]; exists {
		// On the home page a news-text row can precede the same notice row.
		// Keep the first title/order but upgrade its missing date.
		if (*items)[index].Date == "" && date != "" {
			(*items)[index].Date = date
			(*items)[index].PublishedAt = publishedAt
			sflRememberDate(fullURL, rawDate)
		}
		if (*items)[index].Type == "" && typ != "" {
			(*items)[index].Type = typ
		}
		return true
	}

	seen[fullURL] = len(*items)
	*items = append(*items, model.NewsItem{
		Title:       title,
		URL:         fullURL,
		Date:        date,
		PublishedAt: publishedAt,
		Type:        typ,
		ParsedAt:    time.Now(),
	})
	if date != "" || publishedAt != "" {
		sflRememberDate(fullURL, rawDate)
	}
	return true
}

func sflTypeFromSelection(scope *goquery.Selection, doc *goquery.Document) string {
	if scope != nil && scope.Length() > 0 {
		// Do not inspect generic .title/.tit nodes here: on list rows those
		// selectors are commonly the article headline itself.  Only use an
		// explicit product heading or heading element outside the row links.
		for _, selector := range []string{".product-title", ".product-tit", ".product-head"} {
			value := strings.Join(strings.Fields(scope.Find(selector).First().Text()), " ")
			if value != "" && !strings.Contains(value, "查看更多") {
				return value
			}
		}
	}
	if doc != nil {
		return sflTypeFromDocument(doc)
	}
	return ""
}

func sflHomeType(types []string, index int) string {
	if index < 0 || index >= len(types) {
		return ""
	}
	return types[index]
}

func sflTypeFromDocument(doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	rightTop := doc.Find(".righttop").First()
	if rightTop.Length() > 0 {
		var heading string
		rightTop.Children().EachWithBreak(func(_ int, child *goquery.Selection) bool {
			node := child.Get(0)
			if node == nil || node.Data != "span" || child.HasClass("ben") {
				return true
			}
			heading = strings.Join(strings.Fields(child.Text()), " ")
			return false
		})
		if heading != "" && !sflGenericPageLabel(heading) {
			return heading
		}

		candidate := ""
		rightTop.Find(".ben a").Each(func(_ int, link *goquery.Selection) {
			value := strings.Join(strings.Fields(link.Text()), " ")
			if value == "" || value == "首页" || value == "网站首页" || value == "正文" || sflGenericPageLabel(value) {
				return
			}
			candidate = value
		})
		if candidate != "" {
			return candidate
		}
	}
	for _, selector := range []string{".inner-tit h6", ".inner-tit h5", ".position h1", ".list-title", ".list-head h1", ".list-head h2", ".list-head h3"} {
		value := strings.Join(strings.Fields(doc.Find(selector).First().Text()), " ")
		if value != "" && !sflGenericPageLabel(value) {
			return value
		}
	}
	if value := strings.Join(strings.Fields(doc.Find(`meta[name="pageTitle"]`).First().AttrOr("content", "")), " "); value != "" && !sflGenericPageLabel(value) {
		return value
	}
	return ""
}

func sflGenericPageLabel(value string) bool {
	switch strings.TrimSpace(value) {
	case "新闻公告", "新闻动态":
		return true
	default:
		return false
	}
}

func sflArticleURL(baseURL, href string) (string, bool) {
	fullURL := cleanURL(resolveURL(baseURL, href))
	parsed, err := url.Parse(fullURL)
	if err != nil || parsed.Host == "" || !isValidNewsURL(fullURL) {
		return "", false
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || !strings.EqualFold(parsed.Host, base.Host) {
		return "", false
	}
	if !sflArticlePathRe.MatchString(parsed.Path) {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func sflDateFromListItem(item *goquery.Selection) string {
	return sflDateFromText(sflRawDateFromListItem(item))
}

func sflRawDateFromListItem(item *goquery.Selection) string {
	if item == nil || item.Length() == 0 {
		return ""
	}
	for _, selector := range []string{".tim", ".date", ".time", "time"} {
		if value := strings.TrimSpace(item.Find(selector).First().Text()); value != "" {
			if sflDateFromText(value) != "" {
				return value
			}
		}
	}
	return ""
}

func sflDateFromNotice(entry *goquery.Selection) string {
	return sflDateFromText(sflRawDateFromNotice(entry))
}

func sflRawDateFromNotice(entry *goquery.Selection) string {
	if entry == nil || entry.Length() == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(entry.Find("dt").First().Text()), "")
	match := sflNoticeDateRe.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s", match[2], match[3], match[1])
}

func sflDateFromText(text string) string {
	match := sflDateRe.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return ""
	}
	return normalizeDateParts(match[1], match[2], match[3])
}

func sflPublishedAtFromText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return ""
	}
	if match := dateTimeRe.FindString(text); match != "" {
		return normalizePublishedAt(match)
	}
	if match := sflChineseDateTimeRe.FindStringSubmatch(text); match != nil {
		second := match[7]
		if second == "" {
			second = "00"
		}
		return fmt.Sprintf("%s-%02s-%02s %02s:%s:%s", match[1], match[2], match[3], match[4], match[5], second)
	}
	return ""
}

func sflRememberDate(articleURL, date string) {
	key := sflArticleKey(articleURL)
	dateOnly := sflDateFromText(date)
	if key == "" || dateOnly == "" {
		return
	}
	publishedAt := sflPublishedAtFromText(date)
	if publishedAt == "" {
		publishedAt = normalizePublishedAt(dateOnly)
	}
	sflState.Lock()
	sflState.dates[key] = dateOnly
	if publishedAt != "" {
		sflState.publishedAt[key] = publishedAt
	}
	sflState.Unlock()
}

func sflCachedDate(articleURL string) string {
	key := sflArticleKey(articleURL)
	if key == "" {
		return ""
	}
	sflState.RLock()
	date := sflState.dates[key]
	sflState.RUnlock()
	return date
}

func sflCachedPublishedAt(articleURL string) string {
	key := sflArticleKey(articleURL)
	if key == "" {
		return ""
	}
	sflState.RLock()
	publishedAt := sflState.publishedAt[key]
	sflState.RUnlock()
	return publishedAt
}

func sflArticleKey(articleURL string) string {
	parsed, err := url.Parse(articleURL)
	if err != nil {
		return ""
	}
	match := sflArticlePathRe.FindStringSubmatch(parsed.Path)
	if match == nil {
		return ""
	}
	return match[1] + "/" + match[2]
}

func sflRememberPageCount(scope *goquery.Selection) {
	if scope == nil || scope.Length() == 0 {
		return
	}
	count := sflPageCountFromSelection(scope)
	if count < 1 {
		return
	}
	sflState.Lock()
	sflState.pageCounts["xyxw"] = count
	sflState.Unlock()
}

func sflPageCountFromSelection(scope *goquery.Selection) int {
	_, _, pages, ok := sflPageInfoFromSelection(scope)
	if !ok {
		return 0
	}
	return pages
}

func sflPageInfoFromDocument(doc *goquery.Document) (itemCount, currentPage, totalPages int, ok bool) {
	if doc == nil {
		return 0, 0, 0, false
	}
	if counter := doc.Find(".lit [id^='fanye']").First(); counter.Length() > 0 {
		return sflPageInfoFromSelection(counter)
	}
	if counter := doc.Find("[id^='fanye']").First(); counter.Length() > 0 {
		return sflPageInfoFromSelection(counter)
	}
	if pagination := doc.Find(".lit").First(); pagination.Length() > 0 {
		return sflPageInfoFromSelection(pagination)
	}
	return 0, 0, 0, false
}

func sflPageInfoFromSelection(scope *goquery.Selection) (itemCount, currentPage, totalPages int, ok bool) {
	if scope == nil || scope.Length() == 0 {
		return 0, 0, 0, false
	}
	text := sflPaginationText(scope)
	match := sflPageInfoRe.FindStringSubmatch(text)
	if match == nil {
		return 0, 0, 0, false
	}
	itemCount, errItems := strconv.Atoi(match[1])
	currentPage, errCurrent := strconv.Atoi(match[2])
	totalPages, errPages := strconv.Atoi(match[3])
	if errItems != nil || errCurrent != nil || errPages != nil || itemCount < 0 || currentPage < 0 || totalPages < 0 {
		return 0, 0, 0, false
	}
	if totalPages == 0 {
		if itemCount != 0 || currentPage != 0 {
			return 0, 0, 0, false
		}
		return itemCount, currentPage, totalPages, true
	}
	if currentPage < 1 || currentPage > totalPages {
		return 0, 0, 0, false
	}
	return itemCount, currentPage, totalPages, true
}

func sflPaginationText(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	if scope.Is("[id^='fanye']") {
		return strings.Join(strings.Fields(scope.Text()), " ")
	}
	if counter := scope.Find("[id^='fanye']").First(); counter.Length() > 0 {
		return strings.Join(strings.Fields(counter.Text()), " ")
	}
	clone := scope.Clone()
	clone.Find("script, style, noscript").Remove()
	return strings.Join(strings.Fields(clone.Text()), " ")
}

func sflTotalPages(slug string) (int, error) {
	slug = sflTargetSlug(slug)
	if !sflKnownSlug(slug) {
		return 0, fmt.Errorf("sfl: %q is not an audited article-list route", slug)
	}
	baseURL := "http://sfl.swjtu.edu.cn"
	if site := AllSites["sfl"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = strings.TrimRight(site.BaseURL, "/")
	}
	body, err := fetch(fmt.Sprintf("%s/%s.htm", baseURL, slug))
	if err != nil {
		body, err = fetch(fmt.Sprintf("%s/%s.html", baseURL, slug))
		if err != nil {
			return 0, err
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return 0, fmt.Errorf("sfl parse page count: %w", err)
	}
	if doc.Find("ul.list-group.rightnew").Length() == 0 {
		if sflBlockedTextRe.MatchString(strings.ToLower(string(body))) {
			return 0, fmt.Errorf("sfl page %q is a blocked or login response", slug)
		}
		return 0, fmt.Errorf("sfl page %q has no article-list container", slug)
	}
	if _, parseErr := parseSflNews(body, baseURL); parseErr != nil {
		return 0, fmt.Errorf("sfl page %q is not a valid article list: %w", slug, parseErr)
	}
	_, _, count, ok := sflPageInfoFromDocument(doc)
	if !ok {
		return 0, fmt.Errorf("sfl page count is missing or malformed for %s", slug)
	}
	// The source uses 0/0 for the genuine empty research-admissions list.
	// Shared range pagination needs one safe logical page to represent it; no
	// generated page 2 is ever constructed because the cached value is 1.
	if count == 0 {
		count = 1
	}
	sflState.Lock()
	sflState.pageCounts[slug] = count
	sflState.Unlock()
	return count, nil
}

func sflBaseURL() string {
	baseURL := "http://sfl.swjtu.edu.cn"
	if site := AllSites["sfl"]; site != nil && strings.TrimSpace(site.BaseURL) != "" {
		baseURL = strings.TrimRight(site.BaseURL, "/")
	}
	return baseURL
}

func sflFetchArticle(articleURL string) (*model.Article, []byte, error) {
	baseURL := sflBaseURL()
	fullURL, ok := sflArticleURL(baseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("sfl: invalid article URL %q", articleURL)
	}
	body, err := fetch(fullURL)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseSflArticle(body, baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("sfl article %s: %w", fullURL, err)
	}
	article.Images = sflVerifyImages(article.Images, baseURL)
	article.Attachments = sflVerifyAttachments(article.Attachments, baseURL)
	// The detail template has no publication node. Use the date cached from
	// the exact list URL, not the _jsq_ counter embedded in the HTML: that
	// counter is a click statistic and is occasionally off by one article ID.
	sflApplyCachedPublication(article, fullURL)
	if article.Type == "" {
		if parsed, parseErr := url.Parse(fullURL); parseErr == nil {
			if match := sflArticlePathRe.FindStringSubmatch(parsed.Path); match != nil {
				article.Type = sflChannelTypes[match[1]]
			}
		}
	}
	return article, body, nil
}

// The HTML parser only normalizes resource URLs so it remains deterministic
// for fixture tests. A live article fetch must additionally prove that each
// resource is a real image/file rather than a login, CAPTCHA, WAF, or error
// page returned with HTTP 200.
func sflVerifyImages(images []string, baseURL string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := sflResourceURL(baseURL, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		if _, _, err := sflFetchResource(resolved, baseURL, true); err != nil {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

func sflVerifyAttachments(attachments []model.Attachment, baseURL string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved := sflResourceURL(baseURL, attachment.URL)
		if resolved == "" || sflIsCaptchaDownloadURL(resolved) || seen[resolved] {
			continue
		}
		body, _, err := sflFetchResource(resolved, baseURL, false)
		if err != nil {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func sflFetchResource(rawURL, baseURL string, image bool) ([]byte, string, error) {
	resolved := sflResourceURL(baseURL, rawURL)
	if resolved == "" {
		return nil, "", fmt.Errorf("sfl: refusing invalid resource %q", rawURL)
	}
	if !image && sflIsCaptchaDownloadURL(resolved) {
		return nil, "", fmt.Errorf("sfl: refusing CAPTCHA-protected download %q", resolved)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("sfl: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) == 0 || strings.EqualFold(next.URL.Hostname(), via[len(via)-1].URL.Hostname()) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("sfl: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("sfl: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("sfl: read resource %s: %w", resolved, err)
	}
	if len(body) > 32<<20 || !sflResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("sfl: resource %s returned an invalid shell or file", resolved)
	}
	return body, contentType, nil
}

func sflResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	for _, invalid := range []string{"text/html", "application/xhtml+xml", "application/json", "text/xml", "application/xml"} {
		if mediaType == invalid {
			return false
		}
	}
	probe := body
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	textProbe := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(string(probe))), "\ufeff")
	for _, marker := range []string{
		"<!doctype html", "<html", "<head", "<body", "附件下载", "请输入验证码", "验证码下载附件",
		"请先登录", "请登录后", "登录页面", "页面不存在", "404 not found", "access denied",
		"cloudflare", "just a moment", "安全验证", "请求已被阻断",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := sflResourceMagic(body)
	if image {
		return strings.HasPrefix(mediaType, "image/") && sflIsImageMagic(magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "zip") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || strings.Contains(mediaType, "msword") || strings.Contains(mediaType, "ms-excel"):
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values":
		return true
	default:
		return magic != ""
	}
}

func sflIsImageMagic(magic string) bool {
	switch magic {
	case "jpeg", "png", "gif", "webp", "bmp", "tiff":
		return true
	default:
		return false
	}
}

func sflResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}):
		return "zip"
	case len(body) >= 4 && bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x07, 0x08}):
		return "zip"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}):
		return "ole"
	case len(body) >= 3 && bytes.Equal(body[:3], []byte{0xff, 0xd8, 0xff}):
		return "jpeg"
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "png"
	case len(body) >= 6 && (bytes.Equal(body[:6], []byte("GIF87a")) || bytes.Equal(body[:6], []byte("GIF89a"))):
		return "gif"
	case len(body) >= 12 && bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "webp"
	case len(body) >= 2 && bytes.Equal(body[:2], []byte("BM")):
		return "bmp"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{'I', 'I', '*', 0}) || bytes.Equal(body[:4], []byte{'M', 'M', 0, '*'})):
		return "tiff"
	case len(body) >= 7 && bytes.Equal(body[:7], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00}):
		return "rar"
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	default:
		return ""
	}
}

func sflApplyCachedPublication(article *model.Article, articleURL string) {
	if article == nil {
		return
	}
	if date := sflCachedDate(articleURL); date != "" {
		article.Date = date
		if publishedAt := sflCachedPublishedAt(articleURL); publishedAt != "" {
			article.PublishedAt = publishedAt
		}
		normalizeArticlePublishedAt(article)
	}
}

func parseSflArticle(html []byte, baseURL string) (*model.Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("parse sfl article: %w", err)
	}
	if !sflHasArticleShape(doc) {
		return nil, fmt.Errorf("response is not a recognisable sfl article page")
	}
	article, err := parseBodaArticle(html, baseURL)
	if err != nil {
		return nil, err
	}
	if date := sflDateFromText(article.Date); date != "" {
		article.Date = date
	}

	if title := strings.TrimSpace(doc.Find("h1.newtitle2").First().Text()); title != "" {
		article.Title = strings.Join(strings.Fields(title), " ")
	}
	if strings.TrimSpace(article.Title) == "" {
		return nil, fmt.Errorf("sfl article has no title")
	}

	// Some older sfl pages may contain an explicit metadata line. Only inspect
	// metadata containers; a bare date in .v_news_content is an event date,
	// not necessarily the publication date.
	for _, selector := range []string{".news_source", ".article-meta", ".article-info", ".detail-info", ".news-info", ".top-info"} {
		date, publishedAt := sflFixedPublication(doc.Find(selector).First())
		if date == "" {
			continue
		}
		article.Date = date
		if publishedAt != "" {
			article.PublishedAt = publishedAt
		}
		break
	}

	if article.Type == "" {
		article.Type = sflTypeFromDocument(doc)
	}
	sflExtractArticleAttachments(article, doc, baseURL)
	sflCleanArticleResources(article, baseURL)
	normalizeArticlePublishedAt(article)
	return article, nil
}

func sflHasArticleShape(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	title := doc.Find("h1.newtitle2, h1.news_tit, .news-title, .article-title, .detail-title").First()
	content := doc.Find("#vsb_content_501 .v_news_content, #vsb_content .v_news_content, .v_news_content, #vsb_content_501, #vsb_content")
	return title.Length() > 0 && strings.TrimSpace(title.Text()) != "" && content.Length() > 0
}

func sflFixedPublication(scope *goquery.Selection) (date, publishedAt string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	text := strings.Join(strings.Fields(scope.Text()), " ")
	if text == "" {
		return "", ""
	}
	if match := sflLabeledDateRe.FindStringSubmatch(text); match != nil {
		date = normalizeDateParts(match[1], match[2], match[3])
		publishedAt = sflPublishedAtFromText(text)
		if publishedAt == "" {
			publishedAt = normalizePublishedAt(date)
		}
		return date, publishedAt
	}
	// .news_source/.top-info may mark the date with a class rather than a
	// Chinese label. Restrict this fallback to the metadata selection passed by
	// the caller; never run it over the article body.
	if date = sflDateFromText(text); date != "" {
		return date, sflPublishedAtFromText(text)
	}
	return "", ""
}

func sflExtractArticleAttachments(article *model.Article, doc *goquery.Document, baseURL string) {
	if article == nil || doc == nil {
		return
	}
	seen := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		seen[attachment.URL] = true
	}
	doc.Find(".newinfo2 a[href], .attachments a[href], .attachment a[href]").Each(func(_ int, link *goquery.Selection) {
		href := strings.TrimSpace(link.AttrOr("href", ""))
		if href == "" || bodaIsPseudoResourceHref(href) {
			return
		}
		row := link.Closest("li")
		if !bodaLooksLikeAttachmentLink(link, href) && (row.Length() == 0 || !strings.Contains(row.Text(), "附件")) {
			return
		}
		resolved := sflResourceURL(baseURL, href)
		if resolved == "" || sflIsCaptchaDownloadURL(resolved) || seen[resolved] {
			return
		}
		name := strings.TrimSpace(link.AttrOr("download", ""))
		if name == "" {
			name = strings.TrimSpace(link.Text())
		}
		if name == "" {
			name = href
		}
		seen[resolved] = true
		article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
	})
}

func sflCleanArticleResources(article *model.Article, baseURL string) {
	if article == nil {
		return
	}
	images := make([]string, 0, len(article.Images))
	seenImages := make(map[string]bool, len(article.Images))
	for _, raw := range article.Images {
		resolved := sflResourceURL(baseURL, raw)
		if resolved == "" || seenImages[resolved] {
			continue
		}
		seenImages[resolved] = true
		images = append(images, resolved)
	}
	article.Images = images

	attachments := make([]model.Attachment, 0, len(article.Attachments))
	seenAttachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		resolved := sflResourceURL(baseURL, attachment.URL)
		if resolved == "" || sflIsCaptchaDownloadURL(resolved) || seenAttachments[resolved] {
			continue
		}
		seenAttachments[resolved] = true
		attachment.URL = resolved
		attachments = append(attachments, attachment)
	}
	article.Attachments = attachments
}

func sflIsCaptchaDownloadURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && strings.EqualFold(parsed.Path, "/system/_content/download.jsp")
}

func sflResourceURL(baseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || bodaIsPseudoResourceHref(raw) {
		return ""
	}
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	for _, marker := range []string{"captcha", "verify", "login", "signin", "placeholder", "transparent", "spacer", "blank.gif", "loading.gif", "loading.png", "pixel.gif"} {
		if strings.Contains(path, marker) {
			return ""
		}
	}
	return parsed.String()
}

func sflLabeledOrExplicitDate(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if match := sflLabeledDateRe.FindStringSubmatch(text); match != nil {
		return normalizeDateParts(match[1], match[2], match[3])
	}
	return sflDateFromText(text)
}

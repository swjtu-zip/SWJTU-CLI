package scraper

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"swjtu-cli/internal/model"
)

const jwcCanonicalBaseURL = "https://jwc.swjtu.edu.cn"

// jwcSubPages is the set of article-bearing lists found by walking the VATUU
// navigation, the category tabs, and the extra tabs rendered inside the
// category pages.  Static profile pages, file/rule downloads, login pages,
// teaching systems, and links to other hosts are intentionally not feeds.
var jwcSubPages = []string{
	"all",
	"news",
	"notice",
	"teaching",
	"research",
	"practice",
	"experiment",
	"quality",
	"supervision",

	"news/important",
	"news/innovation",
	"news/media",
	"news/video",

	"notice/latest",
	"notice/registration",
	"notice/grades",
	"notice/graduation",
	"notice/certificates",
	"notice/minor",
	"notice/international",
	"notice/graduate-exemption",
	"notice/other",

	"teaching/operation",
	"teaching/plan",
	"teaching/course-selection",
	"teaching/exams",
	"teaching/workload",
	"teaching/foreign-language",
	"teaching/textbooks",
	"teaching/classrooms",

	"research/reform",
	"research/talent-plan",
	"research/courses",
	"research/majors",
	"research/bilingual",
	"research/achievements",
	"research/ideological",

	"practice/operation",
	"practice/innovation-entrepreneurship",
	"practice/graduation-design",
	"practice/internships",
	"practice/competitions",
	"practice/student-innovation",

	"experiment/labs",
	"experiment/teaching",
	"experiment/personalized",
	"experiment/competitions",
	"experiment/demonstration-centers",
	"experiment/virtual-courses",

	"quality/quality-assurance",
	"quality/professional-evaluation",
	"quality/course-evaluation",
	"quality/evaluation-knowledge",
	"quality/first-class-majors",
	"quality/teacher-development",
}

type jwcFeedRoute struct {
	slug         string
	title        string
	bigTypeID    string
	keyWord      string
	fallbackType string
}

// These IDs are the values used by the real VATUU links.  The small tabs use
// keyWord/selectType=smallType; the top-level tabs use bigTypeId/
// selectType=bigType.  Some IDs are reused by VATUU in different selectType
// contexts, so the route kind must not be inferred from the ID alone.
var jwcFeedRoutes = []jwcFeedRoute{
	{slug: "all", title: "全部"},
	{slug: "news", title: "教学动态", bigTypeID: "FB2D32BEAD066637", fallbackType: "新闻动态"},
	{slug: "notice", title: "教务管理", bigTypeID: "61E92EF67418DC54", fallbackType: "教务管理"},
	{slug: "teaching", title: "教学运行", bigTypeID: "0E4BF4D36E232918", fallbackType: "教学运行"},
	{slug: "research", title: "教改研究", bigTypeID: "79FADCFD4573759D", fallbackType: "教学研究"},
	{slug: "practice", title: "实践教学", bigTypeID: "42ED96EE3E089C19", fallbackType: "实践教学"},
	{slug: "experiment", title: "实验教学", bigTypeID: "2CC5A6C6AA4A8CB9", fallbackType: "实验教学"},
	{slug: "quality", title: "质量保障", bigTypeID: "36A0F9DAB7403C19", fallbackType: "质量保障"},
	{slug: "supervision", title: "教学督导", bigTypeID: "EAA4D32165DE589B", fallbackType: "教学督导"},

	{slug: "news/important", title: "要闻及动态", keyWord: "054870155A7061CD", fallbackType: "要闻及动态"},
	{slug: "news/innovation", title: "科创活动相关", keyWord: "6A21C537BA042F30", fallbackType: "科创活动相关"},
	{slug: "news/media", title: "媒体及社会关注", keyWord: "8E3746FC429F906F", fallbackType: "媒体及社会关注"},
	{slug: "news/video", title: "视频新闻", keyWord: "49F06446FD42E242", fallbackType: "视频新闻"},

	{slug: "notice/latest", title: "最新信息发布", keyWord: "4B04AF02FCBBBBF9", fallbackType: "最新信息发布"},
	{slug: "notice/registration", title: "学籍与注册事务", keyWord: "EE416E25D3991FB3", fallbackType: "学籍与注册事务"},
	{slug: "notice/grades", title: "成绩事务管理", keyWord: "881A8F30F8EA0FAC", fallbackType: "成绩事务管理"},
	{slug: "notice/graduation", title: "毕业管理", keyWord: "86EA6DF3E9D79241", fallbackType: "毕业管理"},
	{slug: "notice/certificates", title: "证书及证明管理", keyWord: "3E3D4284CBCEF3A0", fallbackType: "证书及证明管理"},
	{slug: "notice/minor", title: "辅修专业", keyWord: "36A0F9DAB7403C19", fallbackType: "辅修专业"},
	{slug: "notice/international", title: "留学生相关事务", keyWord: "A49BFA21CE2467B7", fallbackType: "留学生相关事务"},
	{slug: "notice/graduate-exemption", title: "免试研究生", keyWord: "65E0C2060B78110E", fallbackType: "免试研究生"},
	{slug: "notice/other", title: "其他公告", keyWord: "A20EEBE85D1379D8", fallbackType: "其他公告"},

	{slug: "teaching/operation", title: "教学运行事务", keyWord: "5D1696C6981179D5", fallbackType: "教学运行事务"},
	{slug: "teaching/plan", title: "教学计划及任务", keyWord: "AC8EB15264D3F09F", fallbackType: "教学计划及任务"},
	{slug: "teaching/course-selection", title: "排课选课事务", keyWord: "61E92EF67418DC54", fallbackType: "排课选课事务"},
	{slug: "teaching/exams", title: "考试安排相关", keyWord: "3C0CB38A818928FE", fallbackType: "考试安排相关"},
	{slug: "teaching/workload", title: "教学工作量相关", keyWord: "37A57AEBA7C0F921", fallbackType: "教学工作量相关"},
	{slug: "teaching/foreign-language", title: "外语等级考试", keyWord: "62F3B43F24ACB54B", fallbackType: "外语等级考试"},
	{slug: "teaching/textbooks", title: "教材建设与管理", keyWord: "EAA4D32165DE589B", fallbackType: "教材建设与管理"},
	{slug: "teaching/classrooms", title: "教室事务管理", keyWord: "F731E139BA9B396D", fallbackType: "教室事务管理"},

	{slug: "research/reform", title: "教学改革与研究", keyWord: "B5EF1775A6A1ECEE", fallbackType: "教学改革与研究"},
	{slug: "research/talent-plan", title: "人才培养计划", keyWord: "176C672645C9E7AB", fallbackType: "人才培养计划"},
	{slug: "research/courses", title: "课程建设管理", keyWord: "F359EC4403709AFF", fallbackType: "课程建设管理"},
	{slug: "research/majors", title: "专业建设管理", keyWord: "D523326EB3917BFB", fallbackType: "专业建设管理"},
	{slug: "research/bilingual", title: "双语教学管理", keyWord: "D2ADC7B457C0E2A7", fallbackType: "双语教学管理"},
	{slug: "research/achievements", title: "教学成果相关", keyWord: "B3EF393FC1F185E0", fallbackType: "教学成果相关"},
	{slug: "research/ideological", title: "思政育人", keyWord: "53526CF4E629057B", fallbackType: "思政育人"},

	{slug: "practice/operation", title: "实践教学相关", keyWord: "61640CD79CCCD5EB", fallbackType: "实践教学相关"},
	{slug: "practice/innovation-entrepreneurship", title: "创新创业教育", keyWord: "1C9BE59E5B866B52", fallbackType: "创新创业教育"},
	{slug: "practice/graduation-design", title: "毕业设计及论文", keyWord: "DF92758312C29666", fallbackType: "毕业设计及论文"},
	{slug: "practice/internships", title: "实习及基地建设", keyWord: "186AA098F4995398", fallbackType: "实习及基地建设"},
	{slug: "practice/competitions", title: "学科竞赛相关", keyWord: "10AFE12FE8876171", fallbackType: "学科竞赛相关"},
	{slug: "practice/student-innovation", title: "大学生科创活动", keyWord: "F37693DABF7E9BD8", fallbackType: "大学生科创活动"},

	{slug: "experiment/labs", title: "实验室建设", keyWord: "80DAAD9DB75A3941", fallbackType: "实验室建设"},
	{slug: "experiment/teaching", title: "实验教学", keyWord: "EA17DE3866C34097", fallbackType: "实验教学"},
	{slug: "experiment/personalized", title: "个性化重点实验室项目", keyWord: "929F7F277D58C759", fallbackType: "个性化重点实验室项目"},
	{slug: "experiment/competitions", title: "课外实验竞赛", keyWord: "EBE56A00ACF08041", fallbackType: "课外实验竞赛"},
	{slug: "experiment/demonstration-centers", title: "示范中心(虚仿中心)", keyWord: "474725BF5729535B", fallbackType: "示范中心(虚仿中心)"},
	{slug: "experiment/virtual-courses", title: "虚仿课程", keyWord: "93353645569D2AD5", fallbackType: "虚仿课程"},

	{slug: "quality/quality-assurance", title: "质量建设与保障", keyWord: "1404E0CD561BF7CF", fallbackType: "质量建设与保障"},
	{slug: "quality/professional-evaluation", title: "专业评估与认证", keyWord: "30781CD41AF044EF", fallbackType: "专业评估与认证"},
	{slug: "quality/course-evaluation", title: "课程评估管理", keyWord: "890AABC3435EF210", fallbackType: "课程评估管理"},
	{slug: "quality/evaluation-knowledge", title: "评估知识学习", keyWord: "4C2FC4049EBBA9F4", fallbackType: "评估知识学习"},
	{slug: "quality/first-class-majors", title: "一流专业建设", keyWord: "04370C01FD8C662C", fallbackType: "一流专业建设"},
	{slug: "quality/teacher-development", title: "教师教学发展", keyWord: "B4F513430B5EBC91", fallbackType: "教师教学发展"},
}

var (
	jwcPageParamNames = []string{
		"jumpPage", "currentPage", "pageNum", "pageNo", "page", "pagenum",
		"pageIndex", "pageNumber", "nowPage", "currPage", "curPage",
	}
	jwcPaginationRe = regexp.MustCompile(`共有记录[^0-9]{0,12}(\d+)[^0-9]{0,12}条.*?当前第[^0-9]{0,12}(\d+)[^0-9]{0,12}页.*?(\d+)[[:space:]]*条[[:space:]]*/[[:space:]]*页.*?共[^0-9]{0,12}(\d+)[^0-9]{0,12}页`)
	jwcDateTimeRe   = regexp.MustCompile(`20\d{2}[\s\x{a0}]*[-/.年][\s\x{a0}]?\d{1,2}[\s\x{a0}]*[-/.月][\s\x{a0}]?\d{1,2}(?:日|号)?[\s\x{a0}]+\d{1,2}[：:]\d{2}(?:[：:]\d{2})?`)
	jwcBareDateRe   = regexp.MustCompile(`20\d{2}[\s\x{a0}]*[-/.年][\s\x{a0}]?\d{1,2}[\s\x{a0}]*[-/.月][\s\x{a0}]?\d{1,2}`)
	jwcArticlePath  = regexp.MustCompile(`(?i)^/vatuu/WebAction$`)
	jwcFileExtRe    = regexp.MustCompile(`(?i)\.(?:doc|docx|xls|xlsx|ppt|pptx|pdf|zip|rar|7z|txt|csv|wps|rtf)(?:$|[?#&])`)
	jwcTypeRe       = regexp.MustCompile(`(?:分类|类别|类型|栏目)[\s\x{a0}]*[:：][\s\x{a0}]*([^\r\n]*)`)
	jwcSourceRe     = regexp.MustCompile(`(?:信息来源|来源)[\s\x{a0}]*[:：][\s\x{a0}]*([^\r\n]*)`)
	jwcCategoryRe   = regexp.MustCompile(`(?:新闻动态|教务管理|教学运行|教学研究|实践教学|实验教学|质量保障|教学督导)[\s\x{a0}]*[-—－]{1,2}[\s\x{a0}]*([^\s|]+)`)
)

func init() {
	// The VATUU homepage is a portal shell: it links to the newsList category
	// pages, system functions (course query, login, file/rule lists), and
	// external teaching systems, but exposes no article rows of its own, so
	// the SDK must not publish an implicit "index" feed for it. The route
	// lookup below still refuses the "index" slug as defense in depth.
	if site := AllSites["jwc"]; site != nil {
		site.NoIndexFeed = true
	}
	registerAdapter("jwc", SiteAdapter{
		SubPages:     jwcSubPages,
		ParseList:    parseAdapter_jwcList,
		ParseArticle: parseAdapter_jwcArticle,
		ListPage:     jwcListPage,
		FetchArticle: jwcFetchArticle,
	})
}

type jwcPageInfo struct {
	totalItems int
	current    int
	perPage    int
	totalPages int
}

func jwcFindRoute(slug string) (jwcFeedRoute, bool) {
	for _, route := range jwcFeedRoutes {
		if route.slug == strings.Trim(strings.TrimSpace(slug), "/") {
			return route, true
		}
	}
	return jwcFeedRoute{}, false
}

// jwcListURL constructs the real VATUU list request. Page one deliberately
// omits the page parameter, matching the server's canonical first-page URL.
func jwcListURL(baseURL, slug string, page int, pageParam string) (string, error) {
	if page < 1 {
		return "", fmt.Errorf("jwc: page must be positive")
	}
	route, ok := jwcFindRoute(slug)
	if !ok {
		return "", fmt.Errorf("jwc: %q is not a registered article feed", slug)
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("jwc: invalid base URL %q", baseURL)
	}
	base.Path = "/vatuu/WebAction"
	base.RawPath = ""
	query := url.Values{}
	query.Set("setAction", "newsList")
	if route.keyWord != "" {
		query.Set("keyWord", route.keyWord)
		query.Set("newsType", "all")
		query.Set("selectType", "smallType")
		query.Set("viewType", "secondStyle")
	} else if route.bigTypeID != "" {
		query.Set("bigTypeId", route.bigTypeID)
		query.Set("newsType", "yes")
		query.Set("selectType", "bigType")
		query.Set("viewType", "secondStyle")
	}
	if page > 1 {
		if pageParam == "" {
			pageParam = "jumpPage"
		}
		query.Set(pageParam, strconv.Itoa(page))
	}
	base.RawQuery = query.Encode()
	return base.String(), nil
}

// jwcListPage validates page numbers against the source counter and probes
// the server's actual pagination parameter. VATUU deployments have changed
// that parameter name over time; accepting a response only when its own
// “当前第 N 页” equals the requested page prevents silently collecting page 1
// repeatedly under an ignored query parameter.
func jwcListPage(slug string, page int) ([]model.NewsItem, int, error) {
	return jwcListPageWithFetcher(jwcCanonicalBaseURL, slug, page, jwcFetchHTML)
}

// jwcListPageWithFetcher contains the deterministic pagination checks used by
// the production adapter and by fixture-backed tests. Keeping the fetcher
// injectable makes it possible to exercise first/middle/last/out-of-range
// pages without turning the unit suite into a live-site dependency.
func jwcListPageWithFetcher(baseURL, slug string, page int, fetcher func(string) ([]byte, error)) ([]model.NewsItem, int, error) {
	if fetcher == nil {
		return nil, 0, fmt.Errorf("jwc: nil HTML fetcher")
	}
	route, ok := jwcFindRoute(slug)
	if !ok {
		return nil, 0, fmt.Errorf("jwc: %q is not a registered article feed", slug)
	}
	firstURL, err := jwcListURL(baseURL, route.slug, 1, "")
	if err != nil {
		return nil, 0, err
	}
	firstBody, err := fetcher(firstURL)
	if err != nil {
		return nil, 0, fmt.Errorf("jwc: fetch %s: %w", route.slug, err)
	}
	firstInfo, err := jwcPaginationFromHTML(firstBody)
	if err != nil {
		return nil, 0, fmt.Errorf("jwc: %s first page: %w", route.slug, err)
	}
	firstItems, err := parseAdapter_jwcList(firstBody, baseURL)
	if err != nil {
		return nil, 0, fmt.Errorf("jwc: %s first page: %w", route.slug, err)
	}
	if page == 1 {
		jwcApplyFallbackType(firstItems, route.fallbackType)
		return firstItems, firstInfo.totalPages, nil
	}
	if page > firstInfo.totalPages {
		return nil, firstInfo.totalPages, fmt.Errorf("jwc: %s page %d exceeds source total %d", route.slug, page, firstInfo.totalPages)
	}

	candidates := make([]string, 0, len(jwcPageParamNames)+1)
	if detected := jwcPageParamFromHTML(firstBody, page); detected != "" {
		candidates = append(candidates, detected)
	}
	for _, candidate := range jwcPageParamNames {
		if !jwcContainsString(candidates, candidate) {
			candidates = append(candidates, candidate)
		}
	}
	var lastErr error
	for _, pageParam := range candidates {
		pageURL, urlErr := jwcListURL(baseURL, route.slug, page, pageParam)
		if urlErr != nil {
			lastErr = urlErr
			continue
		}
		body, fetchErr := fetcher(pageURL)
		if fetchErr != nil {
			lastErr = fetchErr
			continue
		}
		info, infoErr := jwcPaginationFromHTML(body)
		if infoErr != nil {
			lastErr = infoErr
			continue
		}
		if info.current != page {
			lastErr = fmt.Errorf("server returned logical page %d for %s=%d", info.current, pageParam, page)
			continue
		}
		items, parseErr := parseAdapter_jwcList(body, baseURL)
		if parseErr != nil {
			return nil, info.totalPages, fmt.Errorf("jwc: %s page %d: %w", route.slug, page, parseErr)
		}
		if info.totalItems > 0 && len(items) == 0 {
			return nil, info.totalPages, fmt.Errorf("jwc: %s page %d reported %d records but contained no article rows", route.slug, page, info.totalItems)
		}
		jwcApplyFallbackType(items, route.fallbackType)
		return items, info.totalPages, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no page parameter candidate")
	}
	return nil, firstInfo.totalPages, fmt.Errorf("jwc: could not fetch logical page %d of %s: %w", page, route.slug, lastErr)
}

func jwcApplyFallbackType(items []model.NewsItem, fallback string) {
	if fallback == "" {
		return
	}
	for i := range items {
		if strings.TrimSpace(items[i].Type) == "" {
			items[i].Type = fallback
		}
	}
}

func jwcContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func jwcPageParamFromHTML(raw []byte, page int) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return ""
	}
	wanted := strconv.Itoa(page)
	var found string
	doc.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
		href := strings.TrimSpace(html.UnescapeString(anchor.AttrOr("href", "")))
		if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") {
			return true
		}
		resolved := cleanURL(resolveURL(jwcCanonicalBaseURL, href))
		parsed, parseErr := url.Parse(resolved)
		if parseErr != nil {
			return true
		}
		query := parsed.Query()
		for _, name := range jwcPageParamNames {
			if query.Get(name) == wanted {
				found = name
				return false
			}
		}
		return true
	})
	if found != "" {
		return found
	}
	// The live VATUU template submits the selected option from a form field;
	// its javascript:gotoPage(N) links contain no query string to inspect.
	doc.Find("select[name]").EachWithBreak(func(_ int, selectBox *goquery.Selection) bool {
		name := strings.TrimSpace(selectBox.AttrOr("name", ""))
		if !jwcContainsString(jwcPageParamNames, name) {
			return true
		}
		if selectBox.Find("option[value=\""+wanted+"\"]").Length() > 0 {
			found = name
			return false
		}
		return true
	})
	if found != "" {
		return found
	}
	doc.Find("input[name]").EachWithBreak(func(_ int, input *goquery.Selection) bool {
		name := strings.TrimSpace(input.AttrOr("name", ""))
		value := strings.TrimSpace(input.AttrOr("value", ""))
		if value != wanted || !jwcContainsString(jwcPageParamNames, name) {
			return true
		}
		found = name
		return false
	})
	return found
}

func jwcPaginationFromHTML(raw []byte) (jwcPageInfo, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return jwcPageInfo{}, fmt.Errorf("parse pagination HTML: %w", err)
	}
	doc.Find("script, style, noscript").Remove()
	text := strings.Join(strings.Fields(doc.Text()), "")
	match := jwcPaginationRe.FindStringSubmatch(text)
	if match == nil {
		return jwcPageInfo{}, fmt.Errorf("missing source counter (共有记录/当前第/共页)")
	}
	values := make([]int, 4)
	for i := range values {
		values[i], err = strconv.Atoi(match[i+1])
		if err != nil {
			return jwcPageInfo{}, fmt.Errorf("invalid source counter %q", match[i+1])
		}
	}
	info := jwcPageInfo{totalItems: values[0], current: values[1], perPage: values[2], totalPages: values[3]}
	if info.totalItems < 0 || info.current < 1 || info.perPage < 1 {
		return jwcPageInfo{}, fmt.Errorf("invalid source counter values: %#v", info)
	}
	if info.totalPages == 0 && info.totalItems == 0 {
		// VATUU has emitted both 共0页 and 共1页 for a legal empty category.
		// Treat either as one reachable logical page, never as a crawl error.
		info.totalPages = 1
	}
	if info.totalPages < 1 {
		return jwcPageInfo{}, fmt.Errorf("non-empty source has no pages")
	}
	if info.current > info.totalPages {
		return jwcPageInfo{}, fmt.Errorf("current page %d exceeds total %d", info.current, info.totalPages)
	}
	return info, nil
}

func parseAdapter_jwcList(raw []byte, baseURL string) ([]model.NewsItem, error) {
	if err := jwcResponseError(raw); err != nil {
		return nil, err
	}
	info, err := jwcPaginationFromHTML(raw)
	if err != nil {
		return nil, fmt.Errorf("jwc: not a VATUU article list: %w", err)
	}
	if info.totalItems == 0 {
		return []model.NewsItem{}, nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("jwc: parse list HTML: %w", err)
	}
	items := make([]model.NewsItem, 0)
	seen := make(map[string]bool)
	doc.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		if isInNavArea(anchor) {
			return
		}
		fullURL, ok := jwcArticleURL(baseURL, anchor.AttrOr("href", ""))
		if !ok || seen[fullURL] {
			return
		}
		row := jwcListRow(anchor, baseURL)
		title := jwcListTitle(anchor, row)
		if !jwcValidTitle(title) {
			return
		}
		date, publishedAt := jwcDateFromScope(row)
		typ := jwcTypeFromScope(row)
		seen[fullURL] = true
		item := model.NewsItem{Title: title, URL: fullURL, Date: date, Type: typ, ParsedAt: time.Now()}
		if publishedAt != "" && publishedAtHasClock(publishedAt) {
			item.PublishedAt = publishedAt
		}
		items = append(items, item)
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("jwc: source reported %d records but no local article rows were parsed", info.totalItems)
	}
	return items, nil
}

func jwcListRow(anchor *goquery.Selection, baseURL string) *goquery.Selection {
	if anchor == nil || anchor.Length() == 0 {
		return anchor
	}
	var fallback *goquery.Selection
	parent := anchor.Parent()
	for depth := 0; parent != nil && parent.Length() > 0 && depth < 8; depth++ {
		articleURLs := make(map[string]bool)
		parent.Find("a[href]").Each(func(_ int, candidate *goquery.Selection) {
			if fullURL, ok := jwcArticleURL(baseURL, candidate.AttrOr("href", "")); ok {
				articleURLs[fullURL] = true
			}
		})
		if len(articleURLs) == 1 {
			if fallback == nil {
				fallback = parent
			}
			classes := strings.ToLower(parent.AttrOr("class", ""))
			if parent.Is("li, article") || strings.Contains(classes, "item") || strings.Contains(classes, "news") || strings.Contains(classes, "list") || strings.Contains(classes, "row") || strings.Contains(classes, "result") {
				return parent
			}
		}
		parent = parent.Parent()
	}
	if fallback != nil {
		return fallback
	}
	return anchor.Parent()
}

func jwcListTitle(anchor, row *goquery.Selection) string {
	for _, value := range []string{anchor.AttrOr("title", ""), anchor.AttrOr("data-title", "")} {
		if value = jwcCleanText(value); value != "" {
			return value
		}
	}
	if row != nil && row.Length() > 0 {
		for _, selector := range []string{".title", ".news-title", ".news_tit", ".xw-ul-tt", ".line1", "h2", "h3", "h4"} {
			if found := row.Find(selector).First(); found.Length() > 0 {
				clone := found.Clone()
				clone.Find("img, .date, .time, .type, .category, .source, .more").Remove()
				if value := jwcCleanText(clone.Text()); value != "" {
					return value
				}
			}
		}
	}
	clone := anchor.Clone()
	clone.Find("img, .date, .time, .type, .category, .source, .more").Remove()
	return jwcCleanText(clone.Text())
}

func jwcValidTitle(title string) bool {
	title = jwcCleanText(title)
	if len([]rune(title)) < 4 {
		return false
	}
	lower := strings.ToLower(title)
	for _, skip := range []string{"more+", "image", "返回列表", "网站首页", "用户登录", "登录", "注册", "查看更多"} {
		if lower == strings.ToLower(skip) {
			return false
		}
	}
	return true
}

func jwcDateFromScope(scope *goquery.Selection) (string, string) {
	if scope == nil || scope.Length() == 0 {
		return "", ""
	}
	for _, selector := range []string{"time[datetime]", "time", ".date", ".time", ".news-date", ".news_time", ".xw-ul-date"} {
		found := scope.Find(selector).First()
		if found.Length() == 0 {
			continue
		}
		value := strings.TrimSpace(found.AttrOr("datetime", ""))
		if value == "" {
			value = found.Text()
		}
		if date, precise := jwcDateValues(value); date != "" {
			return date, precise
		}
	}
	return jwcDateValues(flattenedText(scope))
}

func jwcDateValues(raw string) (string, string) {
	raw = html.UnescapeString(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
	if raw == "" {
		return "", ""
	}
	if match := jwcDateTimeRe.FindString(raw); match != "" {
		precise := normalizePublishedAt(match)
		if len(precise) >= 10 {
			return precise[:10], precise
		}
	}
	if match := jwcBareDateRe.FindString(raw); match != "" {
		date := normalizePublishedAt(match)
		if len(date) >= 10 {
			return date[:10], ""
		}
	}
	return "", ""
}

func jwcTypeFromScope(scope *goquery.Selection) string {
	if scope == nil || scope.Length() == 0 {
		return ""
	}
	for _, selector := range []string{"[data-type]", ".type", ".category", ".column", ".lanmu", ".news-type", ".newsType"} {
		found := scope.Find(selector).First()
		if found.Length() == 0 {
			continue
		}
		if typ := jwcCleanType(found.AttrOr("data-type", "")); typ != "" {
			return typ
		}
		if typ := jwcCleanType(found.Text()); typ != "" {
			return typ
		}
	}
	text := flattenedText(scope)
	if match := jwcTypeRe.FindStringSubmatch(text); match != nil {
		if typ := jwcCleanType(jwcTrimMetaValue(match[1])); typ != "" {
			return typ
		}
	}
	if match := jwcCategoryRe.FindStringSubmatch(text); match != nil {
		return jwcCleanType(match[1])
	}
	return ""
}

func jwcCleanType(value string) string {
	value = jwcCleanText(value)
	value = strings.TrimSpace(strings.Trim(value, "-—－:：|"))
	if idx := strings.LastIndexAny(value, "-—－"); idx >= 0 {
		value = strings.TrimSpace(value[idx+1:])
	}
	return value
}

// jwcTrimMetaValue replaces the look-ahead that would be tempting in a
// metadata regexp but is not supported by Go's RE2 implementation. VATUU
// puts several labelled fields on one line, so stop a captured value at the
// next known label instead.
func jwcTrimMetaValue(value string) string {
	value = jwcCleanText(value)
	cut := len(value)
	for _, label := range []string{
		"信息来源", "来源", "日期", "发布时间", "发布日期", "分类", "类别", "类型", "栏目", "阅读量", "点击量",
	} {
		for _, suffix := range []string{"：", ":"} {
			if index := strings.Index(value, label+suffix); index >= 0 && index < cut {
				cut = index
			}
		}
	}
	return jwcCleanText(value[:cut])
}

func parseAdapter_jwcArticle(raw []byte, baseURL string) (*model.Article, error) {
	if err := jwcResponseError(raw); err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("jwc: parse article HTML: %w", err)
	}
	_, title := jwcArticleTitle(doc)
	if !jwcValidTitle(title) || strings.Contains(strings.ToLower(title), "vatuu") {
		return nil, fmt.Errorf("jwc: article has no real title")
	}
	content := jwcArticleContent(doc)
	if content == nil || content.Length() == 0 {
		return nil, fmt.Errorf("jwc: article has no VATUU content container")
	}
	contentText := jwcContentText(content)
	if strings.TrimSpace(contentText) == "" {
		return nil, fmt.Errorf("jwc: article content is empty")
	}

	article := &model.Article{Title: title, Content: contentText}
	jwcApplyArticleMeta(doc, article)
	jwcAppendImages(article, content, baseURL)
	jwcAppendAttachments(article, doc, content, baseURL)
	jwcSanitizeContentLinks(article, content, baseURL)
	if htmlContent, htmlErr := content.Html(); htmlErr == nil {
		article.ContentHTML = htmlContent
	}
	jwcNormalizeArticleDate(article)
	if article.Date == "" {
		return nil, fmt.Errorf("jwc: article %q has no publication date", article.Title)
	}
	if article.Type == "" {
		return nil, fmt.Errorf("jwc: article %q has no source category", article.Title)
	}
	return article, nil
}

func jwcArticleTitle(doc *goquery.Document) (*goquery.Selection, string) {
	for _, selector := range []string{
		".news-content > h3.title", ".news-content h3.title", ".news-content .title",
		"h1.news_tit", ".news_tit", ".news-title", ".deta-tt",
		".art-tit h1", ".art-tit h2", ".cont-tit h1", ".cont-tit h2",
		".article-title", ".detail-title", "#newsTitle", "h1",
	} {
		found := doc.Find(selector).First()
		if found.Length() == 0 {
			continue
		}
		if value := jwcCleanText(found.Text()); value != "" {
			return found, value
		}
	}
	for _, selector := range []string{`meta[name="pageTitle"]`, `meta[name="title"]`} {
		value := jwcCleanText(doc.Find(selector).AttrOr("content", ""))
		if value != "" {
			return doc.Find(selector).First(), value
		}
	}
	return nil, ""
}

func jwcArticleContent(doc *goquery.Document) *goquery.Selection {
	selectors := []string{
		// Current VATUU detail pages use the first two selectors. The vsb
		// selectors retain compatibility with the older template used by
		// existing article fixtures; broad .content fallbacks are omitted so
		// navigation/footer shells cannot become an article body.
		".news-content > .content-main", ".news-content .content-main",
		".leftNewsCont .content-main", ".content-main",
		"#vsb_content_501 .v_news_content", ".v_news_content", "#vsb_content_501",
		".news-content-wrapper", ".news_content", ".news-content", ".newsContent",
		".article-content", ".article_content", ".detail-content", ".detail_content",
		"#newsContent", ".newsDetail", ".detail .content",
	}
	for _, selector := range selectors {
		found := doc.Find(selector).First()
		if found.Length() == 0 || isInNavArea(found) {
			continue
		}
		if strings.TrimSpace(jwcContentText(found)) != "" || found.Find("img, table").Length() > 0 {
			return found
		}
	}
	return nil
}

func jwcContentText(content *goquery.Selection) string {
	clone := content.Clone()
	clone.Find("script, style, noscript").Remove()
	parts := make([]string, 0)
	clone.Find("p, h1, h2, h3, h4, h5, h6, li, blockquote, pre, tr").Each(func(_ int, block *goquery.Selection) {
		if block.Find("p, h1, h2, h3, h4, h5, h6, li, blockquote, pre, tr").Length() > 0 {
			return
		}
		if value := jwcCleanText(block.Text()); value != "" {
			parts = append(parts, value)
		}
	})
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	return jwcCleanText(clone.Text())
}

func jwcApplyArticleMeta(doc *goquery.Document, article *model.Article) {
	if doc == nil || article == nil {
		return
	}
	// The live template puts all publication fields in one fixed bar directly
	// below .news-content .title. Read only that bar: the body contains many
	// activity/deadline/meeting dates which are not publication metadata.
	var metadataSel *goquery.Selection
	for _, selector := range []string{
		".news-content > .news-top-bar", ".news-content .news-top-bar",
		".news_source", ".news-source",
	} {
		if found := doc.Find(selector).First(); found.Length() > 0 {
			metadataSel = found
			break
		}
	}
	if metadataSel == nil || metadataSel.Length() == 0 {
		return
	}
	metadata := flattenedText(metadataSel)
	if metadata == "" {
		return
	}
	if match := jwcSourceRe.FindStringSubmatch(metadata); match != nil {
		article.Source = jwcTrimMetaValue(match[1])
	}
	if match := jwcTypeRe.FindStringSubmatch(metadata); match != nil {
		article.Type = jwcCleanType(jwcTrimMetaValue(match[1]))
	}
	if date, precise := jwcDateValues(metadata); date != "" {
		article.Date = date
		article.PublishedAt = precise
	}
}

// jwcNormalizeArticleDate deliberately does not derive PublishedAt from Date.
// A date-only source must remain date-only instead of being silently turned
// into midnight, while a source timestamp keeps the seconds exposed by VATUU.
func jwcNormalizeArticleDate(article *model.Article) {
	if article == nil {
		return
	}
	if article.PublishedAt != "" {
		if precise := normalizePublishedAt(article.PublishedAt); publishedAtHasClock(precise) {
			article.PublishedAt = precise
		} else {
			article.PublishedAt = ""
		}
	}
	if date, _ := jwcDateValues(article.Date); date != "" {
		article.Date = date
	} else if article.Date == "" && publishedAtHasClock(article.PublishedAt) {
		article.Date = article.PublishedAt[:10]
	}
}

func jwcAppendImages(article *model.Article, content *goquery.Selection, baseURL string) {
	seen := make(map[string]bool)
	content.Find("img").Each(func(_ int, image *goquery.Selection) {
		src := ""
		for _, attr := range []string{"src", "data-src", "data-original", "orisrc", "data-lazy-src"} {
			if value := strings.TrimSpace(image.AttrOr(attr, "")); value != "" {
				src = value
				break
			}
		}
		if src == "" || strings.HasPrefix(strings.ToLower(src), "data:") {
			return
		}
		resolved := jwcResourceURL(baseURL, src)
		if resolved == "" || seen[resolved] {
			return
		}
		seen[resolved] = true
		article.Images = append(article.Images, resolved)
		image.SetAttr("src", resolved)
	})
}

func jwcAppendAttachments(article *model.Article, doc *goquery.Document, content *goquery.Selection, baseURL string) {
	seen := make(map[string]bool)
	appendFrom := func(scope *goquery.Selection) {
		if scope == nil || scope.Length() == 0 {
			return
		}
		scope.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
			href := strings.TrimSpace(html.UnescapeString(anchor.AttrOr("href", "")))
			resolved, ok := jwcSameOriginURL(baseURL, href)
			if !ok || seen[resolved] || !jwcAttachmentLink(anchor, resolved) {
				return
			}
			name := jwcAttachmentName(anchor, resolved)
			if name == "" {
				return
			}
			seen[resolved] = true
			anchor.SetAttr("href", resolved)
			article.Attachments = append(article.Attachments, model.Attachment{Name: name, URL: resolved})
		})
	}
	appendFrom(content)
	// VATUU commonly renders its attachment UL beside, rather than inside,
	// .v_news_content. Keep this list bounded so footer download links are not
	// mistaken for files belonging to the article.
	for _, selector := range []string{
		".left-news-details", ".attachments", ".attachment", ".news-attachment",
		".download-list", ".down-attach", ".attach-list", ".article-detail",
	} {
		doc.Find(selector).Each(func(_ int, scope *goquery.Selection) { appendFrom(scope) })
	}
}

// jwcSanitizeContentLinks keeps article-local attachment links in the
// archived HTML and resolves ordinary external links. A local HTML shell is
// not a resource, so its href is removed while its visible label is retained.
func jwcSanitizeContentLinks(article *model.Article, content *goquery.Selection, baseURL string) {
	if content == nil || content.Length() == 0 {
		return
	}
	attachments := make(map[string]bool, len(article.Attachments))
	for _, attachment := range article.Attachments {
		attachments[attachment.URL] = true
	}
	content.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(html.UnescapeString(anchor.AttrOr("href", "")))
		resolved, sameOrigin := jwcSameOriginURL(baseURL, href)
		if !sameOrigin {
			if external := jwcResourceURL(baseURL, href); external != "" {
				anchor.SetAttr("href", external)
			} else {
				anchor.RemoveAttr("href")
			}
			return
		}
		if attachments[resolved] {
			anchor.SetAttr("href", resolved)
			return
		}
		if jwcUnsafeContentLink(baseURL, resolved) {
			anchor.RemoveAttr("href")
		}
	})
}

// jwcResourceURL accepts ordinary HTTP(S) resources, including external
// article images and links. Attachment collection still applies the stricter
// same-origin rule before a URL can be downloaded as a file.
func jwcResourceURL(baseURL, raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
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
	resourceText := strings.ToLower(parsed.Path + "?" + parsed.RawQuery)
	for _, marker := range []string{
		"/captcha", "verifycode", "verification", "createimage", "getcode",
		"placeholder", "transparent", "spacer", "blank.gif", "loading.gif",
		"pixel.gif", "access-denied", "waf",
	} {
		if strings.Contains(resourceText, marker) {
			return ""
		}
	}
	parsed.Fragment = ""
	parsed.RawQuery = parsed.Query().Encode()
	return parsed.String()
}

func jwcUnsafeContentLink(baseURL, resolved string) bool {
	if resolved == "" {
		return true
	}
	if _, ok := jwcArticleURL(baseURL, resolved); ok {
		return false
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return true
	}
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".htm") || strings.HasSuffix(path, ".shtml") {
		return true
	}
	return strings.EqualFold(parsed.Query().Get("setAction"), "download")
}

// jwcSanitizeContentHTML is applied after live resource verification. Parser
// output may know that a link looks like a file, but only the live response
// can prove that it is not a CAPTCHA/login HTML shell. Candidate attachments
// are kept separate so failed downloads lose their href in archived HTML.
func jwcSanitizeContentHTML(raw, baseURL string, images []string, candidateAttachments, attachments []model.Attachment) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	validImages := make(map[string]bool, len(images))
	for _, image := range images {
		if resolved := jwcResourceURL(baseURL, image); resolved != "" {
			validImages[resolved] = true
		}
	}
	candidateFiles := make(map[string]bool, len(candidateAttachments))
	for _, attachment := range candidateAttachments {
		if resolved, ok := jwcSameOriginURL(baseURL, attachment.URL); ok {
			candidateFiles[resolved] = true
		}
	}
	validFiles := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if resolved, ok := jwcSameOriginURL(baseURL, attachment.URL); ok {
			validFiles[resolved] = true
		}
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div id=\"jwc-content-root\">" + raw + "</div>"))
	if err != nil {
		return ""
	}
	root := doc.Find("#jwc-content-root").First()
	root.Find("img").Each(func(_ int, image *goquery.Selection) {
		resolved := ""
		for _, attr := range []string{"src", "data-src", "data-original", "orisrc", "data-lazy-src"} {
			if candidate := jwcResourceURL(baseURL, image.AttrOr(attr, "")); candidate != "" && validImages[candidate] {
				resolved = candidate
				break
			}
		}
		if resolved == "" {
			image.Remove()
			return
		}
		image.SetAttr("src", resolved)
		for _, attr := range []string{"data-src", "data-original", "orisrc", "data-lazy-src"} {
			image.RemoveAttr(attr)
		}
	})
	root.Find("a[href]").Each(func(_ int, anchor *goquery.Selection) {
		href := strings.TrimSpace(html.UnescapeString(anchor.AttrOr("href", "")))
		if strings.HasPrefix(href, "#") {
			return
		}
		resolved, sameOrigin := jwcSameOriginURL(baseURL, href)
		if sameOrigin && jwcAttachmentLink(anchor, resolved) {
			if !candidateFiles[resolved] || !validFiles[resolved] {
				anchor.RemoveAttr("href")
				return
			}
			anchor.SetAttr("href", resolved)
			return
		}
		if external := jwcResourceURL(baseURL, href); external != "" {
			if sameOrigin && jwcUnsafeContentLink(baseURL, external) {
				anchor.RemoveAttr("href")
				return
			}
			anchor.SetAttr("href", external)
			return
		}
		anchor.RemoveAttr("href")
	})
	rendered, err := root.Html()
	if err != nil {
		return ""
	}
	return rendered
}

func jwcAttachmentLink(anchor *goquery.Selection, resolved string) bool {
	if anchor == nil || anchor.Length() == 0 || resolved == "" {
		return false
	}
	u, err := url.Parse(resolved)
	if err != nil {
		return false
	}
	query := u.Query()
	lower := strings.ToLower(resolved)
	if strings.EqualFold(query.Get("setAction"), "download") && query.Get("fileId") != "" {
		return true
	}
	if strings.Contains(lower, "downloadattachurl") || strings.Contains(lower, "download.jsp") {
		return true
	}
	if anchor.AttrOr("download", "") != "" {
		return true
	}
	if jwcFileExtRe.MatchString(lower) || jwcFileExtRe.MatchString(strings.ToLower(anchor.AttrOr("name", ""))) {
		return true
	}
	return false
}

func jwcAttachmentName(anchor *goquery.Selection, resolved string) string {
	for _, value := range []string{
		anchor.AttrOr("download", ""), anchor.AttrOr("data-filename", ""),
		anchor.AttrOr("name", ""), anchor.AttrOr("title", ""), anchor.Text(),
	} {
		if value = jwcCleanText(value); value != "" {
			return value
		}
	}
	if parsed, err := url.Parse(resolved); err == nil {
		if name := strings.TrimPrefix(parsed.Path, "/"); name != "" {
			return name
		}
		if id := parsed.Query().Get("fileId"); id != "" {
			return "file-" + id
		}
	}
	return ""
}

func jwcSameOriginURL(baseURL, raw string) (string, bool) {
	resolved := cleanURL(resolveURL(baseURL, raw))
	parsed, err := url.Parse(resolved)
	base, baseErr := url.Parse(baseURL)
	if err != nil || baseErr != nil || parsed.Host == "" || base.Host == "" || parsed.Scheme == "" {
		return "", false
	}
	if (strings.ToLower(parsed.Scheme) != "http" && strings.ToLower(parsed.Scheme) != "https") || (strings.ToLower(base.Scheme) != "http" && strings.ToLower(base.Scheme) != "https") {
		return "", false
	}
	if !jwcURLAuthorityEqual(base, parsed) {
		return "", false
	}
	parsed.RawQuery = parsed.Query().Encode()
	return parsed.String(), true
}

func jwcURLAuthorityEqual(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Host, right.Host)
}

func jwcArticleURL(baseURL, raw string) (string, bool) {
	resolved, ok := jwcSameOriginURL(baseURL, html.UnescapeString(strings.TrimSpace(raw)))
	if !ok {
		return "", false
	}
	parsed, err := url.Parse(resolved)
	if err != nil || !jwcArticlePath.MatchString(parsed.Path) {
		return "", false
	}
	query := parsed.Query()
	if !strings.EqualFold(query.Get("setAction"), "newsDetail") || strings.TrimSpace(query.Get("newsId")) == "" {
		return "", false
	}
	return resolved, true
}

func jwcCleanText(value string) string {
	return strings.Join(strings.Fields(html.UnescapeString(strings.TrimSpace(value))), " ")
}

func jwcResponseError(raw []byte) error {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return fmt.Errorf("jwc: parse response HTML: %w", err)
	}
	title := strings.ToLower(jwcCleanText(doc.Find("title").First().Text()))
	for _, marker := range []string{"captcha", "验证码", "cloudflare", "just a moment", "access denied", "forbidden", "404", "500", "系统错误", "页面不存在", "安全验证"} {
		if strings.Contains(title, marker) {
			return fmt.Errorf("jwc: error/login/verification response (%s)", title)
		}
	}
	clone := doc.Clone()
	clone.Find("script, style, noscript").Remove()
	visible := strings.ToLower(jwcCleanText(clone.Text()))
	for _, marker := range []string{
		"请输入验证码", "验证码下载附件", "请先登录", "请登录后", "页面不存在",
		"access denied", "just a moment", "cf-chl-", "请求已被阻断", "安全威胁",
		"没有访问当前栏目的权限", "您没有访问", "您目前无权限", "非校内范围", "请登陆系统",
		"404 not found", "500 internal server error",
	} {
		if strings.Contains(visible, marker) {
			return fmt.Errorf("jwc: error/login/verification response")
		}
	}
	return nil
}

func jwcFetchArticle(articleURL string) (*model.Article, []byte, error) {
	canonical, ok := jwcArticleURL(jwcCanonicalBaseURL, articleURL)
	if !ok {
		return nil, nil, fmt.Errorf("jwc: refusing non-local or non-detail URL %q", articleURL)
	}
	body, err := jwcFetchHTML(canonical)
	if err != nil {
		return nil, nil, err
	}
	article, err := parseAdapter_jwcArticle(body, jwcCanonicalBaseURL)
	if err != nil {
		return nil, nil, err
	}
	candidateAttachments := append([]model.Attachment(nil), article.Attachments...)
	article.Images = jwcVerifyImages(article.Images, canonical)
	article.Attachments = jwcVerifyAttachmentsAt(article.Attachments, canonical)
	article.ContentHTML = jwcSanitizeContentHTML(article.ContentHTML, canonical, article.Images, candidateAttachments, article.Attachments)
	return article, body, nil
}

// jwcVerifyImages and jwcVerifyAttachmentsAt are intentionally part of the
// live article fetch path, not the HTML-only parser. A resource is exposed
// only after its own response has passed HTTP status, Content-Type, shell and
// magic-signature checks. Failed resource GETs do not invalidate an otherwise
// valid article; they are omitted from the resource fields and HTML.
func jwcVerifyImages(images []string, referer string) []string {
	verified := make([]string, 0, len(images))
	seen := make(map[string]bool, len(images))
	for _, raw := range images {
		resolved := jwcResourceURL(referer, raw)
		if resolved == "" || seen[resolved] {
			continue
		}
		body, contentType, err := jwcFetchResource(resolved, referer, true)
		if err != nil || !jwcResourceBodyIsUsable(contentType, body, resolved, true) {
			continue
		}
		seen[resolved] = true
		verified = append(verified, resolved)
	}
	return verified
}

// jwcVerifyAttachments is kept as a small compatibility wrapper for callers
// and tests that exercise the canonical download endpoint directly.
func jwcVerifyAttachments(attachments []model.Attachment) []model.Attachment {
	return jwcVerifyAttachmentsAt(attachments, jwcCanonicalBaseURL)
}

func jwcVerifyAttachmentsAt(attachments []model.Attachment, referer string) []model.Attachment {
	verified := make([]model.Attachment, 0, len(attachments))
	seen := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		resolved, ok := jwcSameOriginURL(referer, attachment.URL)
		if !ok || seen[resolved] {
			continue
		}
		body, contentType, err := jwcFetchResource(resolved, referer, false)
		if err != nil || !jwcResourceBodyIsUsable(contentType, body, attachment.Name+" "+resolved, false) {
			continue
		}
		attachment.URL = resolved
		attachment.Size = strconv.Itoa(len(body))
		seen[resolved] = true
		verified = append(verified, attachment)
	}
	return verified
}

func jwcFetchAttachment(rawURL string) ([]byte, error) {
	body, _, err := jwcFetchResource(rawURL, jwcCanonicalBaseURL, false)
	return body, err
}

const jwcMaxResourceBytes = 32 << 20

func jwcFetchResource(rawURL, referer string, image bool) ([]byte, string, error) {
	resolved := ""
	if image {
		resolved = jwcResourceURL(referer, rawURL)
	} else {
		var ok bool
		resolved, ok = jwcSameOriginURL(referer, rawURL)
		if !ok {
			return nil, "", fmt.Errorf("jwc: refusing non-local attachment %q", rawURL)
		}
	}
	if resolved == "" {
		return nil, "", fmt.Errorf("jwc: refusing invalid resource %q", rawURL)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", fmt.Errorf("jwc: refusing non-HTTP resource %q", resolved)
	}
	req, err := http.NewRequest(http.MethodGet, resolved, nil)
	if err != nil {
		return nil, "", fmt.Errorf("jwc: create resource request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	if image {
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "application/octet-stream,application/pdf,application/zip,*/*;q=0.8")
	}
	if strings.TrimSpace(referer) != "" {
		req.Header.Set("Referer", referer)
	}
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("jwc: refusing non-HTTP resource redirect")
		}
		if !jwcURLAuthorityEqual(parsed, next.URL) {
			return fmt.Errorf("jwc: refusing cross-site resource redirect")
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("jwc: fetch resource %s: %w", resolved, err)
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, contentType, fmt.Errorf("jwc: resource %s: HTTP %d", resolved, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, jwcMaxResourceBytes+1))
	if err != nil {
		return nil, contentType, fmt.Errorf("jwc: read resource %s: %w", resolved, err)
	}
	if len(body) > jwcMaxResourceBytes || !jwcResourceBodyIsUsable(contentType, body, resolved, image) {
		return nil, contentType, fmt.Errorf("jwc: resource %s returned an invalid Content-Type, shell, or magic signature", resolved)
	}
	return body, contentType, nil
}

func jwcAttachmentBodyIsUsable(contentType string, body []byte) bool {
	return jwcResourceBodyIsUsable(contentType, body, "", false)
}

func jwcResourceBodyIsUsable(contentType string, body []byte, nameOrURL string, image bool) bool {
	if len(body) == 0 {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil && parsed != "" {
		mediaType = strings.ToLower(parsed)
	}
	if mediaType == "" {
		return false
	}
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
		"<!doctype html", "<html", "<head", "<body", "cloudflare", "just a moment",
		"cf-chl-", "请输入验证码", "验证码下载附件", "请先登录", "请登录后", "请求已被阻断", "安全威胁",
	} {
		if strings.Contains(textProbe, marker) {
			return false
		}
	}

	magic := jwcResourceMagic(body)
	if image {
		return jwcImageTypeMatchesMagic(mediaType, magic)
	}
	lowerName := strings.ToLower(nameOrURL)
	switch {
	case strings.Contains(lowerName, ".jpg") || strings.Contains(lowerName, ".jpeg") || strings.Contains(lowerName, ".png") || strings.Contains(lowerName, ".gif") || strings.Contains(lowerName, ".webp") || strings.HasPrefix(mediaType, "image/"):
		return jwcImageTypeMatchesMagic(mediaType, magic)
	case strings.Contains(lowerName, ".pdf") || mediaType == "application/pdf" || strings.HasSuffix(mediaType, "+pdf"):
		return magic == "pdf"
	case strings.Contains(lowerName, ".docx") || strings.Contains(lowerName, ".xlsx") || strings.Contains(lowerName, ".pptx") || strings.Contains(mediaType, "wordprocessingml") || strings.Contains(mediaType, "spreadsheetml") || strings.Contains(mediaType, "presentationml"):
		return magic == "zip"
	case strings.Contains(lowerName, ".doc") || strings.Contains(lowerName, ".xls") || strings.Contains(lowerName, ".ppt") || mediaType == "application/msword" || mediaType == "application/vnd.ms-excel" || mediaType == "application/vnd.ms-powerpoint":
		return magic == "ole"
	case strings.Contains(lowerName, ".zip") || strings.Contains(lowerName, ".rar") || strings.Contains(lowerName, ".7z") || mediaType == "application/zip" || mediaType == "application/x-rar-compressed" || mediaType == "application/x-7z-compressed":
		return magic == "zip" || magic == "rar" || magic == "7z"
	case mediaType == "text/plain" || mediaType == "text/csv" || mediaType == "text/tab-separated-values":
		return true
	default:
		return magic != ""
	}
}

func jwcImageTypeMatchesMagic(mediaType, magic string) bool {
	switch mediaType {
	case "image/jpeg", "image/jpg", "image/pjpeg":
		return magic == "jpeg"
	case "image/png":
		return magic == "png"
	case "image/gif":
		return magic == "gif"
	case "image/webp":
		return magic == "webp"
	case "image/bmp":
		return magic == "bmp"
	case "image/tiff":
		return magic == "tiff"
	case "image/svg+xml":
		return magic == "svg"
	default:
		return false
	}
}

func jwcResourceMagic(body []byte) string {
	switch {
	case bytes.HasPrefix(body, []byte("%PDF-")):
		return "pdf"
	case len(body) >= 4 && (bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x03, 0x04}) || bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x05, 0x06}) || bytes.Equal(body[:4], []byte{0x50, 0x4b, 0x07, 0x08})):
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
	case len(body) >= 8 && bytes.Equal(body[:8], []byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x01, 0x00}):
		return "rar"
	case len(body) >= 6 && bytes.Equal(body[:6], []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}):
		return "7z"
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	if len(trimmed) > 0 {
		probe := trimmed
		if len(probe) > 4096 {
			probe = probe[:4096]
		}
		lower := bytes.ToLower(probe)
		if bytes.Contains(lower, []byte("<svg")) {
			return "svg"
		}
	}
	return ""
}

func jwcFetchHTML(rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (strings.ToLower(parsed.Scheme) != "http" && strings.ToLower(parsed.Scheme) != "https") || !strings.EqualFold(parsed.Host, "jwc.swjtu.edu.cn") {
		return nil, fmt.Errorf("jwc: refusing non-local URL %q", rawURL)
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("jwc: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	client := *defaultClient
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if scheme := strings.ToLower(next.URL.Scheme); scheme != "http" && scheme != "https" {
			return fmt.Errorf("jwc: refusing non-HTTP article redirect")
		}
		if len(via) == 0 || jwcURLAuthorityEqual(parsed, next.URL) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jwc: fetch %s: %w", rawURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwc: fetch %s: HTTP %d", rawURL, response.StatusCode)
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") && !strings.Contains(contentType, "application/xhtml+xml") {
		return nil, fmt.Errorf("jwc: expected HTML from %s, got %s", rawURL, contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("jwc: read %s: %w", rawURL, err)
	}
	return body, nil
}

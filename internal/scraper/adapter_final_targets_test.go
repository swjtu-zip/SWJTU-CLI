package scraper

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

type finalTargetRoundTripper func(*http.Request) (*http.Response, error)

func (f finalTargetRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFinalTargetAdaptersExposeAuditedFeeds(t *testing.T) {
	want := map[string][]string{
		"talent": {"xwzx/rczc", "xwzx/tzgg", "xwzx/xwdt", "xwzx/xzzx"},
		"wkjsc":  {"tzgg", "wkxw", "xsdt", "gzzd", "xzzx", "bszn"},
		"xb":     {"xxfb/tzgg", "xxfb/xwdt", "cyxz", "fwzn/gflc", "fwzn/glwj", "zdgf/bgswj", "zdgf/xjwj"},
		"xczx":   {"tzxw/tzgg", "tzxw/xwdt", "tzxw/xydt", "xsyt/xsfx", "xsyt/xsjl", "xsyt/zjtd", "ktyj/kfkt", "ktyj/yjcg", "ktyj/yjxm", "cyzx/xmch", "cyzx/zxfw"},
		"xg":     xgSubPages,
		"xgh":    {"xwzx1", "tzgg", "jcdt", "jgfc", "szyl", "ghdj", "gzzd", "hdyx1", "bszn", "xzzx"},
		"xuncha": {"gzdt", "sjjs", "xxgongkai", "zcfg", "zcfg/tlztxx", "zcfg/dnfg", "zcfg/xxzd", "zyjh", "jyjl/tszs"},
		"xyb":    {"xwdt1", "xwdt1/djgz", "xwdt1/tzgg", "xwdt1/xwzx", "xwdt1/xwzx/jj", "xwdt1/xwzx/xy", "xyzh/qnxyh", "xyzh/xyfc"},
		"ygb":    {"xwzx/xwdt", "xwzx/tzgg", "xwzx/dcxy", "xwzx/hdyg", "xwzx/ssfc", "xwzx/gsxx", "djyl1/xzzg"},
		"zzxy":   {"xyyw", "xwdt", "tzgg", "xsgz/xsdt", "djjgh/djgz/djfc", "szkc", "cgzs", "xsgg"},
	}
	for id, feeds := range want {
		site := AllSites[id]
		if site == nil {
			t.Fatalf("site %q is not configured", id)
		}
		if !reflect.DeepEqual(site.SubPages, feeds) {
			t.Errorf("%s feeds = %#v, want %#v", id, site.SubPages, feeds)
		}
		if site.ParseFunc == nil || site.ParseArticleFunc == nil {
			t.Errorf("%s parser hooks are incomplete", id)
		}
		if id == "xg" {
			if site.ListPageFunc == nil || site.ArticleFetchFunc == nil {
				t.Errorf("xg dynamic fetch hooks are incomplete")
			}
		} else if site.PageURL == nil || site.TotalPagesFunc == nil {
			t.Errorf("%s pagination hooks are incomplete", id)
		}
	}
}

func TestFinalTargetAdaptersParseLiveListShapes(t *testing.T) {
	cases := []struct {
		id, typ, date, published, title, href string
		html                                  string
	}{
		{
			id: "talent", typ: "人才政策", date: "2026-08-31", published: "2026-08-31 14:03:00",
			title: "人才政策测试通知", href: "/info/1001/9001.htm",
			html: `<meta name="pageTitle" content="人才政策"><ul class="news-list news-list-container news-else-container"><li class="list-item"><a href="/info/1001/9001.htm"><span class="title line1">人才政策测试通知</span><span class="f-s-20">2026年8月31日 14:03</span></a></li></ul>`,
		},
		{
			id: "wkjsc", typ: "通知公告", date: "2026-08-30", published: "2026-08-30",
			title: "文科建设处测试通知公告", href: "/info/1002/9002.htm",
			html: `<div id="tzgg"><ul class="news_ul1"><li><span class="date">2026-08-30</span><a href="/info/1002/9002.htm">文科建设处测试通知公告</a></li></ul></div>`,
		},
		{
			id: "xb", typ: "通知公告", date: "2026-08-29", published: "2026-08-29 09:12:00",
			title: "校长办公室测试通知公告", href: "/info/1003/9003.htm",
			html: `<meta name="pageTitle" content="通知公告"><div class="news-list-box"><ul><li><a href="/info/1003/9003.htm"><span class="new-l_b_title">校长办公室测试通知公告</span><span class="new-list_date">2026-08-29 09:12</span></a></li></ul></div>`,
		},
		{
			id: "xczx", typ: "通知公告", date: "2026-08-28", published: "2026-08-28 08:05:00",
			title: "乡村振兴研究院测试通知", href: "/info/1004/9004.htm",
			html: `<meta name="pageTitle" content="通知公告"><div class="main_conRCb"><ul><li><span>2026年8月28日 08:05</span><a href="/info/1004/9004.htm"><em>乡村振兴研究院测试通知</em></a></li></ul></div><div id="fanye">共9条 1/2</div>`,
		},
		{
			id: "xgh", typ: "通知公告", date: "2026-08-27", published: "2026-08-27 10:11:00",
			title: "校工会测试通知公告", href: "/info/1005/9005.htm",
			html: `<meta name="pageTitle" content="通知公告"><div class="right"><ul class="list"><li><a href="/info/1005/9005.htm">校工会测试通知公告</a><span class="info">发布时间：2026-08-27 10:11</span></li></ul></div>`,
		},
		{
			id: "xuncha", typ: "党内法规", date: "2026-08-26", published: "2026-08-26 11:22:00",
			title: "巡察工作党内法规测试", href: "/info/1006/9006.htm",
			html: `<meta name="pageTitle" content="党内法规"><div class="conlistComm"><dl><dt><span class="tag">[党内法规]</span><a href="/info/1006/9006.htm">巡察工作党内法规测试</a></dt><dd>时间：2026年8月26日 11:22</dd></dl></div>`,
		},
		{
			id: "xyb", typ: "新闻资讯", date: "2026-08-25", published: "2026-08-25",
			title: "校友工作办公室测试新闻", href: "/info/1007/9007.htm",
			html: `<meta name="pageTitle" content="新闻资讯"><div class="list_con"><ul><li><a href="/info/1007/9007.htm"><span class="infoText">校友工作办公室测试新闻</span><span class="locationsj">2026-08-25</span></a></li></ul></div>`,
		},
		{
			id: "ygb", typ: "公示信息", date: "2026-08-24", published: "2026-08-24 07:44:00",
			title: "研究生工作部测试公示信息", href: "/info/1008/9008.htm",
			html: `<meta name="pageTitle" content="公示信息"><div class="newsList"><ul><li><a href="/info/1008/9008.htm"><span class="newsItem">研究生工作部测试公示信息</span><span class="newsDate">2026-08-24 07:44</span></a></li></ul></div>`,
		},
		{
			id: "zzxy", typ: "思政课程", date: "2026-08-23", published: "2026-08-23",
			title: "马克思主义学院思政课程测试", href: "/info/1009/9009.htm",
			html: `<meta name="pageTitle" content="思政课程"><div class="conlistComm"><dl><dt><a href="/info/1009/9009.htm">马克思主义学院思政课程测试</a></dt><dd>时间：2026-08-23</dd></dl></div>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			items, err := AllSites[tc.id].ParseFunc([]byte(tc.html), "https://example.test")
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 {
				t.Fatalf("got %d items, want one: %#v", len(items), items)
			}
			item := items[0]
			if item.Title != tc.title || item.URL != "https://example.test"+tc.href || item.Type != tc.typ || item.Date != tc.date || item.PublishedAt != tc.published {
				t.Fatalf("item = (title=%q,url=%q,type=%q,date=%q,published=%q), want (%q,%q,%q,%q,%q)", item.Title, item.URL, item.Type, item.Date, item.PublishedAt, tc.title, "https://example.test"+tc.href, tc.typ, tc.date, tc.published)
			}
		})
	}
}

func TestFinalTargetAdaptersParseBodyAndOutsideAttachments(t *testing.T) {
	ids := []string{"talent", "wkjsc", "xb", "xczx", "xgh", "xuncha", "xyb", "ygb", "zzxy"}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			articleType := "通知公告"
			html := `<html><head><meta name="pageTitle" content="详情测试标题"></head><body>
<div class="list-title">通知公告</div><div class="article-title">详情测试标题</div>
<div class="news_source"><span>日期：2026-08-22 12:34</span></div>
<div id="vsb_content_501"><div class="v_news_content"><p>这是必须保留的正文精确文本。</p></div></div>
<div class="attachments"><a href="/files/meeting.pdf" download="meeting.pdf">下载</a></div>
</body></html>`
			if id == "wkjsc" {
				html = `<html><head><meta name="pageTitle" content="详情测试标题"></head><body>
<h2>通知公告</h2><div id="div_title"><span id="lb_title">详情测试标题</span><span id="lb_hits">[2026-08-22 12:34 来源 测试来源]</span></div>
<div id="div_wenzi"><div id="vsb_content"><div class="v_news_content"><p>这是必须保留的正文精确文本。</p></div></div></div>
<div class="attachments"><a href="/files/meeting.pdf" download="meeting.pdf">下载</a></div>
</body></html>`
			}
			if id == "xuncha" {
				articleType = "党内法规"
				html = strings.Replace(html, `<div class="list-title">通知公告</div>`, `<div class="list-title">党内法规</div>`, 1)
			}
			if id == "zzxy" {
				html = strings.Replace(html, `<div class="list-title">通知公告</div>`, `<div class="right"><div class="title"><h3>通知公告</h3></div></div>`, 1)
			}
			article, err := AllSites[id].ParseArticleFunc([]byte(html), "https://example.test")
			if err != nil {
				t.Fatal(err)
			}
			if article.Title != "详情测试标题" || article.Date != "2026-08-22" || article.PublishedAt != "2026-08-22 12:34:00" {
				t.Fatalf("metadata = title=%q,date=%q,published=%q", article.Title, article.Date, article.PublishedAt)
			}
			if article.Type != articleType {
				t.Fatalf("type = %q, want %s", article.Type, articleType)
			}
			if !strings.Contains(article.Content, "正文精确文本") {
				t.Fatalf("body missing: %q", article.Content)
			}
			if len(article.Attachments) != 1 || article.Attachments[0].Name != "meeting.pdf" || article.Attachments[0].URL != "https://example.test/files/meeting.pdf" {
				t.Fatalf("attachments = %#v", article.Attachments)
			}
		})
	}
}

func TestFinalTargetHomepageFixturesExposeScopedItems(t *testing.T) {
	cases := []struct {
		id, file string
		min      int
	}{
		{id: "talent", file: "talent.html", min: 1},
		{id: "wkjsc", file: "wkjsc.html", min: 1},
		{id: "xb", file: "xb.html", min: 1},
		{id: "xczx", file: "xczx.html", min: 1},
		{id: "xyb", file: "xyb.html", min: 1},
		{id: "ygb", file: "ygb.html", min: 1},
		{id: "zzxy", file: "zzxy.html", min: 1},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			body, err := os.ReadFile("testdata/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			items, err := AllSites[tc.id].ParseFunc(body, AllSites[tc.id].BaseURL)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) < tc.min {
				t.Fatalf("got %d homepage items, want at least %d", len(items), tc.min)
			}
		})
	}
}

func TestXGDynamicListPaginationDetailAndFileRows(t *testing.T) {
	var requestedPageTwo bool
	serverURL := "https://xg-test.invalid"
	oldClient := defaultClient
	defaultClient = &http.Client{Transport: finalTargetRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/web/Home/PushNewsList":
			if r.URL.Query().Get("page") == "2" {
				if r.URL.Query().Get("PushPNodeNum") != "0109" {
					return nil, fmt.Errorf("missing source node")
				}
				requestedPageTwo = true
				body = `<div class="block-ctxlist"><div class="block-ctxlist-item"><a href="/web/Home/Detail?id=2"><span class="title">第二页动态通知</span></a><span class="date">2026-07-08 08:02</span></div></div>`
			} else {
				body = `<div class="block-ctxlist"><div class="block-ctxlist-item"><a href="/web/Home/Detail?id=1"><span class="title">第一页动态通知</span></a><span class="date">2026-07-09 09:01</span></div></div><div class="pages"><a href="/web/Home/PushNewsList?page=2&amp;PushPNodeNum=0109">2</a></div><div>总共有130条记录</div>`
			}
		case "/web/Home/Detail":
			body = `<div class="breadcrumb">通知公告</div><div class="detail-content-title"><h3>动态详情标题</h3><p>发布时间：2026-07-09 09:08</p><p>信息来源：学工部</p></div><div class="detail-content-text"><p>动态详情正文精确文本。</p><img src="/images/detail.jpg"></div><div class="detail-files"><a href="/service/uploadserver/down/getfile?file=abc&amp;name=动态附件.pdf" download="动态附件.pdf">下载</a></div>`
		case "/service/uploadserver/down/getfile":
			body = "file"
		default:
			return nil, fmt.Errorf("unexpected request path %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}
	defer func() { defaultClient = oldClient }()

	site := AllSites["xg"]
	oldBase, oldNoCache := site.BaseURL, NoCache
	defer func() {
		site.BaseURL = oldBase
		NoCache = oldNoCache
		ClearCache()
	}()
	site.BaseURL = serverURL + "/web"
	NoCache = true
	ClearCache()

	items, total, err := xgListPage(xgSubPages[0], 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || !requestedPageTwo || len(items) != 1 || items[0].Title != "第二页动态通知" || items[0].Date != "2026-07-08" || items[0].PublishedAt != "2026-07-08 08:02:00" || items[0].Type != "通知公告" {
		t.Fatalf("page result = total=%d requested=%v items=%#v", total, requestedPageTwo, items)
	}

	fileItems, err := parseAdapter_xgList([]byte(`<div class="block-list"><a href="/service/uploadserver/down/getfile?file=abc&amp;name=表格.xlsx">下载</a></div>`), site.BaseURL)
	if err != nil || len(fileItems) != 1 || fileItems[0].Title != "表格.xlsx" {
		t.Fatalf("file list = items=%#v err=%v", fileItems, err)
	}

	articleURL := serverURL + "/web/Home/Detail?id=1"
	article, raw, err := xgFetchArticle(articleURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || article.Title != "动态详情标题" || article.Type != "通知公告" || article.Date != "2026-07-09" || article.PublishedAt != "2026-07-09 09:08:00" {
		t.Fatalf("dynamic article = title=%q,type=%q,date=%q,published=%q raw=%d", article.Title, article.Type, article.Date, article.PublishedAt, len(raw))
	}
	if !strings.Contains(article.Content, "动态详情正文精确文本") || len(article.Images) != 1 || article.Images[0] != serverURL+"/images/detail.jpg" {
		t.Fatalf("dynamic resources = content=%q images=%#v", article.Content, article.Images)
	}
	if len(article.Attachments) != 1 || article.Attachments[0].Name != "动态附件.pdf" || article.Attachments[0].URL != serverURL+"/service/uploadserver/down/getfile?file=abc&name=动态附件.pdf" {
		t.Fatalf("dynamic attachments = %#v", article.Attachments)
	}
}

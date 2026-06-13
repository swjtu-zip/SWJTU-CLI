package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"swjtu-cli/internal/model"
	"swjtu-cli/internal/scraper"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "news":
		cmdNews()
	case "read":
		cmdRead()
	case "download":
		cmdDownload()
	case "download-all":
		cmdDownloadAll()
	case "lib":
		cmdLib()
	case "sites":
		cmdSites()
	case "test-all":
		cmdTestAll()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("SWJTU-CLI — 西南交通大学校园信息工具")
	fmt.Println()
	fmt.Println("用法:")
	fmt.Println("  swjtu-cli news                        聚合所有站点新闻")
	fmt.Println("  swjtu-cli news <分类>                 聚合某类站点新闻")
	fmt.Println("  swjtu-cli news <站点ID>               查看某站点新闻")
	fmt.Println("  swjtu-cli news <站点ID> <子分类slug>  查看某站点子分类")
	fmt.Println("  swjtu-cli read <url>                  阅读文章详情")
	fmt.Println("  swjtu-cli download <url> [dir]        下载单个文件")
	fmt.Println("  swjtu-cli download-all <url> [dir]    下载文章全部附件")
	fmt.Println("  swjtu-cli sites [分类]                列出所有站点")
	fmt.Println("  swjtu-cli test-all                    测试所有站点")
	fmt.Println()
	fmt.Println("  swjtu-cli lib news [--page N]        图书馆新闻")
	fmt.Println("  swjtu-cli lib db                     数据库导航")
	fmt.Println("  swjtu-cli lib search <keyword>       统一检索 (全文)")
	fmt.Println("  swjtu-cli lib search <keyword> title 按题名检索")
	fmt.Println("  swjtu-cli lib search <keyword> author 按作者检索")
	fmt.Println("  swjtu-cli lib book <keyword>         馆藏图书检索")
	fmt.Println("  swjtu-cli lib journal <keyword>      期刊论文检索")
	fmt.Println()
	fmt.Println("历史查询:")
	fmt.Println("  --page N          查看第N页")
	fmt.Println("  --from YYYY-MM-DD 从指定日期开始")
	fmt.Println("  --to   YYYY-MM-DD 到指定日期结束")
	fmt.Println("  --max-pages N     最多翻N页 (默认10)")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  swjtu-cli news --page 2               交大要闻第2页")
	fmt.Println("  swjtu-cli news civil tzgg --from 2026-01-01 --to 2026-03-31")
	fmt.Println("  swjtu-cli news --from 2026-06-01      6月以来所有新闻")
	fmt.Println()
	fmt.Println("新闻分类: jdyw mtjd ybdt sjmt tzgg xshd (校级)")
	fmt.Println("站点分类: college party admin unit institute (聚合)")
	fmt.Println("用 'swjtu-cli sites' 查看全部站点ID和子分类")
}

// parseFlags extracts --page, --from, --to, --max-pages from args.
// Returns cleaned args (without flags) and flag values.
func parseFlags(args []string) ([]string, int, string, string, int) {
	var cleaned []string
	page := 1
	from := ""
	to := ""
	maxPages := 10

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--page":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &page)
				i++
			}
		case "--from":
			if i+1 < len(args) {
				from = args[i+1]
				i++
			}
		case "--to":
			if i+1 < len(args) {
				to = args[i+1]
				i++
			}
		case "--max-pages":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &maxPages)
				i++
			}
		default:
			cleaned = append(cleaned, args[i])
		}
	}
	return cleaned, page, from, to, maxPages
}

// cmdNews handles all listing.
func cmdNews() {
	args, page, from, to, maxPages := parseFlags(os.Args[1:])

	// No args: aggregate all sites
	if len(args) < 2 {
		if from != "" || to != "" {
			fmt.Println("📰 历史新闻聚合\n")
			items, success, fail := scraper.AggregateNewsRange("", from, to, maxPages)
			scraper.PrintAggResult(items, success, fail, true)
		} else {
			fmt.Println("📰 聚合所有站点新闻\n")
			items, success, fail := scraper.AggregateNews("", 5)
			scraper.PrintAggResult(items, success, fail, true)
		}
		return
	}

	arg := args[1]

	// Check if it's a news category slug (jdyw, mtjd, etc.)
	if catName, ok := scraper.NewsCategories[arg]; ok {
		if from != "" || to != "" {
			fmt.Printf("📰 %s (历史: %s ~ %s)\n\n", catName, orDefault(from, "最早"), orDefault(to, "最新"))
			items, err := scraper.NewsSite.ListNewsRange(arg, orDefault(from, "2000-01-01"), orDefault(to, "2099-12-31"), maxPages)
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
				os.Exit(1)
			}
			printNewsList(items)
		} else if page > 1 {
			fmt.Printf("📰 %s (第%d页)\n\n", catName, page)
			items, err := scraper.NewsSite.ListNewsPage(arg, page)
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
				os.Exit(1)
			}
			printNewsList(items)
		} else {
			fmt.Printf("📰 %s\n\n", catName)
			items, err := scraper.NewsSite.ListNews(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
				os.Exit(1)
			}
			printNewsList(items)
		}
		return
	}

	// Check if it's a site category (college, party, admin, unit, institute)
	if catName, ok := scraper.CategoryNames[arg]; ok {
		if from != "" || to != "" {
			fmt.Printf("📰 %s 历史新闻\n\n", catName)
			items, success, fail := scraper.AggregateNewsRange(arg, orDefault(from, "2000-01-01"), orDefault(to, "2099-12-31"), maxPages)
			scraper.PrintAggResult(items, success, fail, true)
		} else {
			fmt.Printf("📰 聚合%s新闻\n\n", catName)
			items, success, fail := scraper.AggregateNews(arg, 5)
			scraper.PrintAggResult(items, success, fail, true)
		}
		return
	}

	// Check if it's a site ID
	if site, ok := scraper.AllSites[arg]; ok {
		slug := "index"
		if len(args) > 2 {
			slug = args[2]
		}
		if from != "" || to != "" {
			fmt.Printf("📰 %s [%s] 历史: %s ~ %s\n\n", site.Name, slug, orDefault(from, "最早"), orDefault(to, "最新"))
			items, err := site.ListNewsRange(slug, orDefault(from, "2000-01-01"), orDefault(to, "2099-12-31"), maxPages)
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
				os.Exit(1)
			}
			printNewsList(items)
		} else if page > 1 {
			fmt.Printf("📰 %s [%s] 第%d页\n\n", site.Name, slug, page)
			items, err := site.ListNewsPage(slug, page)
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
				os.Exit(1)
			}
			printNewsList(items)
		} else {
			fmt.Printf("📰 %s [%s]\n\n", site.Name, slug)
			items, err := site.ListNews(slug)
			if err != nil {
				fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
				os.Exit(1)
			}
			printNewsList(items)
		}
		return
	}

	fmt.Fprintf(os.Stderr, "❌ 未知参数: %s\n", arg)
	fmt.Fprintln(os.Stderr, "   用 'swjtu-cli sites' 查看所有站点ID")
	os.Exit(1)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func cmdRead() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "❌ 请提供文章URL")
		os.Exit(1)
	}
	artURL := os.Args[2]

	// Detect which site this URL belongs to
	site := detectSite(artURL)
	fmt.Printf("📖 正在读取文章...\n\n")

	art, err := site.GetArticle(artURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 读取失败: %v\n", err)
		os.Exit(1)
	}
	printArticle(art)
}

func cmdDownload() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "❌ 请提供文件URL")
		os.Exit(1)
	}
	fileURL := os.Args[2]
	outDir := "downloads"
	if len(os.Args) > 3 {
		outDir = os.Args[3]
	}
	fmt.Printf("⬇️  正在下载: %s\n", fileURL)
	if err := downloadFile(fileURL, outDir); err != nil {
		fmt.Fprintf(os.Stderr, "❌ 下载失败: %v\n", err)
		os.Exit(1)
	}
}

func cmdDownloadAll() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "❌ 请提供文章URL")
		os.Exit(1)
	}
	artURL := os.Args[2]
	outDir := "downloads"
	if len(os.Args) > 3 {
		outDir = os.Args[3]
	}

	site := detectSite(artURL)
	fmt.Printf("📖 正在获取文章附件...\n\n")
	art, err := site.GetArticle(artURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📌 %s\n\n", art.Title)
	total, downloaded := 0, 0

	if len(art.Attachments) > 0 {
		fmt.Printf("📎 附件 (%d个):\n", len(art.Attachments))
		for _, att := range art.Attachments {
			total++
			fmt.Printf("  ⬇️  %s\n", att.Name)
			if err := downloadFileAs(att.URL, outDir, att.Name); err != nil {
				fmt.Printf("     ❌ %v\n", err)
			} else {
				downloaded++
			}
		}
	}
	if len(art.Images) > 0 {
		fmt.Printf("\n📷 图片 (%d张):\n", len(art.Images))
		for _, img := range art.Images {
			total++
			fmt.Printf("  ⬇️  %s\n", img)
			if err := downloadFile(img, outDir); err != nil {
				fmt.Printf("     ❌ %v\n", err)
			} else {
				downloaded++
			}
		}
	}
	if total == 0 {
		fmt.Println("  (无附件)")
	} else {
		fmt.Printf("\n📊 下载完成: %d/%d\n", downloaded, total)
	}
}

func cmdSites() {
	filter := ""
	if len(os.Args) > 2 {
		filter = os.Args[2]
	}

	// Group by category
	groups := make(map[string][]string)
	for id, site := range scraper.AllSites {
		if filter != "" && site.Category != filter {
			continue
		}
		groups[site.Category] = append(groups[site.Category], id)
	}

	cats := []string{"college", "party", "admin", "unit", "institute"}
	for _, cat := range cats {
		ids := groups[cat]
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		catName := scraper.CategoryNames[cat]
		fmt.Printf("\n📋 %s (%d)\n", catName, len(ids))
		for _, id := range ids {
			site := scraper.AllSites[id]
			subTag := ""
			if len(site.SubPages) > 0 {
				subTag = fmt.Sprintf(" [子分类: %s]", strings.Join(site.SubPages, ", "))
			}
			fmt.Printf("  %-14s %s%s\n", id, site.Name, subTag)
		}
	}

	if filter == "" {
		fmt.Printf("\n📰 校级站点:\n")
		fmt.Printf("  %-14s %s\n", "news", "西南交通大学新闻网")
		for slug, name := range scraper.NewsCategories {
			fmt.Printf("  %-14s %s\n", slug, name)
		}
	}
}

func cmdTestAll() {
	fmt.Println("🧪 测试所有站点...")
	fmt.Println(strings.Repeat("=", 70))

	total, success, fail := 0, 0, 0
	var failures []string

	// Test news categories
	fmt.Println("\n📡 校级站点:")
	for slug, name := range scraper.NewsCategories {
		total++
		items, err := scraper.NewsSite.ListNews(slug)
		if err != nil {
			fail++
			failures = append(failures, fmt.Sprintf("  ❌ [%s] %s: %v", slug, name, err))
			fmt.Printf("  ❌ %-12s %s - %v\n", slug, name, err)
		} else {
			success++
			fmt.Printf("  ✅ %-12s %s (%d条)\n", slug, name, len(items))
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Test all sites
	cats := []string{"college", "party", "admin", "unit", "institute"}
	for _, cat := range cats {
		catName := scraper.CategoryNames[cat]
		fmt.Printf("\n🏫 %s:\n", catName)
		for id, site := range scraper.AllSites {
			if site.Category != cat {
				continue
			}
			total++
			items, err := site.ListNews("index")
			if err != nil {
				fail++
				failures = append(failures, fmt.Sprintf("  ❌ [%s] %s: %v", id, site.Name, err))
				fmt.Printf("  ❌ %-14s %s - %v\n", id, site.Name, err)
			} else {
				success++
				fmt.Printf("  ✅ %-14s %s (%d条)\n", id, site.Name, len(items))
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Test article detail
	fmt.Println("\n📄 文章详情:")
	testArticleDetail(&total, &success, &fail, &failures)

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("📊 结果: %d/%d 成功, %d 失败\n", success, total, fail)
	if len(failures) > 0 {
		fmt.Println("\n失败列表:")
		for _, f := range failures {
			fmt.Println(f)
		}
	}
}

func testArticleDetail(total, success, fail *int, failures *[]string) {
	items, err := scraper.NewsSite.ListNews("jdyw")
	if err != nil || len(items) == 0 {
		fmt.Println("  ⚠️  跳过")
		return
	}
	*total++
	art, err := scraper.NewsSite.GetArticle(items[0].URL)
	if err != nil {
		*fail++
		*failures = append(*failures, fmt.Sprintf("  ❌ 文章详情: %v", err))
		fmt.Printf("  ❌ 文章详情: %v\n", err)
	} else {
		*success++
		fmt.Printf("  ✅ 文章详情: %s (%d字, %d图, %d附件)\n", art.Title, len(art.Content), len(art.Images), len(art.Attachments))
		if len(art.Images) > 0 {
			*total++
			if err := downloadFile(art.Images[0], "test_downloads"); err != nil {
				*fail++
				fmt.Printf("  ❌ 图片下载: %v\n", err)
			} else {
				*success++
				fmt.Printf("  ✅ 图片下载成功\n")
			}
		}
	}
}

// detectSite returns the appropriate BodaSite for a URL.
func detectSite(url string) *scraper.BodaSite {
	for _, site := range scraper.AllSites {
		if strings.Contains(url, site.BaseURL) || strings.Contains(url, strings.TrimPrefix(site.BaseURL, "http://")) || strings.Contains(url, strings.TrimPrefix(site.BaseURL, "https://")) {
			return site
		}
	}
	return scraper.NewsSite
}

func printNewsList(items []model.NewsItem) {
	if len(items) == 0 {
		fmt.Println("  (无结果)")
		return
	}
	for i, item := range items {
		date := item.Date
		if date == "" {
			date = "----.--.--"
		}
		fmt.Printf("  %2d. [%s] %s\n", i+1, date, item.Title)
		fmt.Printf("      %s\n", item.URL)
	}
	fmt.Printf("\n共 %d 条\n", len(items))
}

func printArticle(art *model.Article) {
	fmt.Printf("📌 %s\n", art.Title)
	fmt.Printf("   来源: %s  日期: %s\n", art.Source, art.Date)
	if art.Author != "" {
		fmt.Printf("   作者: %s", art.Author)
		if art.Photographer != "" {
			fmt.Printf("  摄影: %s", art.Photographer)
		}
		if art.Editor != "" {
			fmt.Printf("  编辑: %s", art.Editor)
		}
		fmt.Println()
	}
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println(art.Content)
	if len(art.Images) > 0 {
		fmt.Println(strings.Repeat("─", 60))
		fmt.Printf("📷 图片 (%d张):\n", len(art.Images))
		for i, img := range art.Images {
			fmt.Printf("  %d. %s\n", i+1, img)
		}
	}
	if len(art.Attachments) > 0 {
		fmt.Println(strings.Repeat("─", 60))
		fmt.Printf("📎 附件 (%d个):\n", len(art.Attachments))
		for i, att := range art.Attachments {
			fmt.Printf("  %d. %s\n     %s\n", i+1, att.Name, att.URL)
		}
	}
	if art.PrevURL != "" {
		fmt.Printf("\n⬅️  %s\n   %s\n", art.PrevTitle, art.PrevURL)
	}
	if art.NextURL != "" {
		fmt.Printf("➡️  %s\n   %s\n", art.NextTitle, art.NextURL)
	}
}

func downloadFile(fileURL, outDir string) error {
	parts := strings.Split(fileURL, "/")
	filename := parts[len(parts)-1]
	if filename == "" {
		filename = "download"
	}
	return downloadFileAs(fileURL, outDir, filename)
}

func downloadFileAs(fileURL, outDir, filename string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	outPath := filepath.Join(outDir, filename)

	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	fmt.Printf("   💾 %s (%d bytes)\n", outPath, len(data))
	return nil
}

func toJSON(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// cmdLib handles library commands.
func cmdLib() {
	if len(os.Args) < 3 {
		printUsage()
		os.Exit(1)
	}

	client := scraper.NewLibClient()
	sub := os.Args[2]

	switch sub {
	case "news":
		// Library news from SWJTU news site
		fmt.Printf("📚 图书馆新闻\n\n")
		items, err := scraper.LibNewsSite.ListNews("tzgg")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
			os.Exit(1)
		}
		var libItems []model.NewsItem
		for _, item := range items {
			if strings.Contains(item.Title, "图书馆") || strings.Contains(item.Title, "数据库") ||
				strings.Contains(item.Title, "期刊") || strings.Contains(item.Title, "借阅") ||
				strings.Contains(item.Title, "读书") || strings.Contains(item.Title, "文献") {
				libItems = append(libItems, item)
			}
		}
		if len(libItems) == 0 {
			libItems = items
		}
		scraper.PrintLibNews(libItems)

	case "db":
		fmt.Println("📚 数据库导航\n")
		items, err := client.GetDatabases()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ 获取失败: %v\n", err)
			os.Exit(1)
		}
		scraper.PrintLibDatabases(items)

	case "search":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "❌ 请提供检索关键词")
			os.Exit(1)
		}
		keyword := os.Args[3]
		searchType := scraper.SearchTypeFullText
		if len(os.Args) > 4 {
			switch os.Args[4] {
			case "title":
				searchType = scraper.SearchTypeTitle
			case "author":
				searchType = scraper.SearchTypeAuthor
			case "keyword":
				searchType = scraper.SearchTypeKeywords
			case "journal":
				searchType = scraper.SearchTypeJournal
			}
		}
		fmt.Printf("🔍 统一检索: %s\n\n", keyword)
		results, total, err := client.Search(keyword, searchType, 1, 20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ 检索失败: %v\n", err)
			os.Exit(1)
		}
		scraper.PrintSearchResults(results, total, keyword)

	case "book":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "❌ 请提供检索关键词")
			os.Exit(1)
		}
		keyword := os.Args[3]
		fmt.Printf("📖 馆藏图书检索: %s\n\n", keyword)
		// Use title search with type=163161 for books
		results, total, err := client.Search(keyword, scraper.SearchTypeTitle, 1, 20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ 检索失败: %v\n", err)
			os.Exit(1)
		}
		scraper.PrintSearchResults(results, total, keyword)

	case "journal":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "❌ 请提供检索关键词")
			os.Exit(1)
		}
		keyword := os.Args[3]
		fmt.Printf("📰 期刊论文检索: %s\n\n", keyword)
		results, total, err := client.Search(keyword, scraper.SearchTypeJournal, 1, 20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ 检索失败: %v\n", err)
			os.Exit(1)
		}
		scraper.PrintSearchResults(results, total, keyword)

	default:
		fmt.Fprintf(os.Stderr, "❌ 未知子命令: %s\n", sub)
		fmt.Fprintln(os.Stderr, "   可用: news, db, search, book, journal")
		os.Exit(1)
	}
}

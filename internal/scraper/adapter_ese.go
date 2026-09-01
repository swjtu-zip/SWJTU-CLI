package scraper

import (
	"fmt"
	"strings"
)

// ese.swjtu.edu.cn numbers generated list pages from oldest to newest while
// the site presents page 1 as the newest page. Keep the site's feeds and URL
// mapping in this adapter instead of changing the shared Boda implementation.
func init() {
	registerAdapter("ese", SiteAdapter{
		SubPages: []string{
			"index/xwgg/hjyw",
			"index/xwgg/tzgg1/qb",
			"index/xwgg/tzgg1/dzbg",
			"index/xwgg/tzgg1/bks",
			"index/xwgg/tzgg1/yjs",
			"index/xwgg/tzgg1/kygz",
			"index/xwgg/tzgg1/xsgz",
			"index/xwgg/tzgg1/qt",
			"kxyj/xsdt",
			"xsgz/txdt",
		},
		ParseList:    parseEseNews,
		ParseArticle: parseEseArticle,
		PageURL:      esePageURL,
	})
}

// esePageURL maps logical pages (1 = newest) to ESE's reverse-numbered files.
// The first page is always the unsuffixed list page, as on the live site.
func esePageURL(baseURL, slug string, page, totalPages int, extension string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	slug = strings.Trim(slug, "/")
	if page <= 1 || totalPages <= 1 {
		return fmt.Sprintf("%s/%s%s", baseURL, slug, extension)
	}
	filePage := totalPages - page + 1
	if filePage < 1 {
		filePage = 1
	}
	return fmt.Sprintf("%s/%s/%d%s", baseURL, slug, filePage, extension)
}

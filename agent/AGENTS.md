# SWJTU-CLI — Architecture & Agent Guide

## Project Overview

Go CLI tool that helps SWJTU students access campus data via terminal.
- **News scraping**: 70 secondary unit websites (博达 CMS + custom parsers)
- **Library API**: Academic literature search via 维普智图 API gateway
- **Designed to expand**: 教务, 邮件, 一网通办, etc.

## Quick Reference

- **Site registry**: `internal/scraper/sites.go` (site metadata; 70 sites)
- **Generic parser**: `internal/scraper/boda.go` (博达 CMS universal parser + utility functions)
- **Site adapters**: `internal/scraper/adapter_<siteid>.go` (one list/article adapter per site)
- **Library API**: `internal/scraper/library.go` (维普智图 API client)
- **Cache**: `internal/scraper/cache.go` (5-minute TTL in-memory cache)
- **HTTP client**: `internal/scraper/client.go` (with cache integration)
- **Browser session**: `internal/browser/browser.go` (dedicated headed Chromium profile)
- **Models**: `internal/model/news.go`
- **Broken sites**: `agent/broken-sites.md`

## Directory Layout

```
SWJTU-CLI/
├── agent/
│   ├── AGENTS.md          # this file
│   └── broken-sites.md    # sites that are down/unsupported
├── cmd/swjtu-cli/
│   └── main.go            # CLI entrypoint
├── internal/
│   ├── browser/
│   │   └── browser.go       # dedicated browser session and rendered HTML capture
│   ├── scraper/
│   │   ├── boda.go         # universal 博达 CMS parser + utility functions
│   │   ├── boda_test.go    # parser unit tests (9 tests)
│   │   ├── cache.go        # HTTP cache (5-min TTL)
│   │   ├── client.go       # HTTP client with cache
│   │   ├── adapter_<siteid>.go # 每站独立的列表/详情 adapter
│   │   ├── library.go      # 图书馆API client (维普智图)
│   │   ├── adapter_registry_test.go # 全站 adapter 覆盖测试
│   │   ├── sites.go        # site metadata registry
│   │   ├── agg.go          # aggregation (concurrent fetching)
│   │   └── testdata/       # HTML/JSON snapshots for unit tests
│   ├── model/
│   │   └── news.go         # NewsItem, Article structs
│   └── oa/                 # future: 教务, 邮件 adapters
├── go.mod
└── go.sum
```

## Parser Architecture

### Generic Parser (boda.go)

Handles most 博达 CMS sites automatically with 5 patterns:
1. **Pattern 1**: `<li class="flex">` with `span.time` + `a[href]`
2. **Pattern 2**: `.list_news li`, `.news_cont li`, `.xw-ul li`, etc.
3. **Pattern 3**: `a[href*='info/']` (last resort)
4. **Pattern 4**: `a[href*='newsDetail']` (scai-like UUID links)
5. Date extraction: `.time`, `span.time`, `em`, `.date`, `.s2-date`, `time`, etc.

### Site Adapters

Every site has its own `adapter_<siteid>.go`, registered through `registerAdapter`. The
adapter may call shared low-level helpers, but site-specific HTML selectors and date rules
stay local to that site. Examples of larger adapters include:

| Site | File | Reason |
|------|------|--------|
| civil | `adapter_civil.go` | homepage cards and reverse-numbered list pages |
| ctt | `adapter_ctt.go` | separate category/homepage modules and generated numbering |
| dqxy | `adapter_dqxy.go` | homepage cards and reverse-numbered list pages |
| gsee | `adapter_gsee.go` | news lists and academic schedule entries |
| sist | `adapter_sist.go` | Vue shell/API payload distinction |
| wkjsc | `adapter_wkjsc.go` | explicit dated rows, legacy detail metadata |

### Utility Functions (boda.go)

- `cleanTitle()`: removes date prefixes, collapses whitespace, strips ">>"
- `isValidNewsTitle()`: filters nav links, section headers, skip patterns
- `isValidNewsURL()`: filters external domains (uses `://` prefix for boundary matching)
- `isInNavArea()`: checks if element is inside header/nav/footer/sidebar/banner
- `resolveURL()`: resolves relative URLs
- `cleanURL()`: removes trailing `%7D` artifacts

## Cache System (cache.go)

- **TTL**: 5 minutes
- **Storage**: in-memory `map[string]cacheEntry`
- **Thread-safe**: `sync.RWMutex`
- **API**: `FetchWithCache(url, nocache)`, `ClearCache()`, `CacheSize()`
- **CLI flags**: `--nocache` bypasses cache

## CLI Flags

| Flag | Description |
|------|-------------|
| `--page N` | View page N |
| `--from YYYY-MM-DD` | Filter from date |
| `�to YYYY-MM-DD` | Filter to date |
| `--max-pages N` | Max pages to fetch (default 10) |
| `--detail-date` | Fetch missing dates from article detail pages |
| `--nocache` | Bypass cache, force fresh data |

## Browser-assisted authentication

For business systems without an API, `auth login <url>` launches a dedicated
headed Chrome/Chromium profile. The user completes CAS login, CAPTCHA, QR-code,
SMS, or other second-factor steps in that browser. `auth fetch <url>` reuses
that profile and returns the rendered page HTML.

- The CLI never asks for or stores the user's password.
- The dedicated profile is separate from the user's normal browser profile.
- Authentication cookies remain in the browser profile; they are not copied into
  the scraper HTTP client.
- This is a generic browser foundation. Each business system still needs its
  own page/API adapter for structured data extraction.
- Set `SWJTU_CLI_BROWSER` when Chrome/Chromium is not discoverable on `PATH`.

## Library API (library.go)

Uses 维普智图 API gateway (lib.swjtu.edu.cn):
- **Auth**: JWT token from `/jwt/api/Auth/AccessToken` (orgCode: "swjtu")
- **Databases**: `/navigation/api/navigation/get-all` (CSSCI, CSCD, SCI, etc.)
- **News**: `/news/api/front/news/page` (library news)
- **Search**: `/articlesearch/api/search/asset-search@4` (academic literature)
  - `keywords` must be empty string; filtering via `rule` parameter
  - `ruleBody`: `U=@U_{random}_0_f` (random variable name)
  - `ruleParameters`: `[{key: "@U_{random}_0_f", value: "search term"}]`
  - Field symbols: U=全文, T=题名, K=关键词, A=作者, S=摘要, P=刊名
  - `requestAggsType`: integer (1=SIMPLE, 2=COMPLEX, 3=CLUSTER)

## Unit Tests

Tests use saved HTML/JSON snapshots from `testdata/`, including:
- `TestJdywParse`: 交大要闻 — dates, titles, URLs correct
- `TestCivilParse`: 土木学院 — no banner content (习近平回信 filtered)
- `TestClxyParse`: 材料学院 — no nav links, no external links
- `TestScyshjParse`: 营商环境 — no date prefixes in titles
- `TestYgbParse: 研工部 — img title extraction
- `TestXczxParse`: 乡村振兴 — no date prefixes
- `TestEseParse`: 环境学院 — dedicated parser, s2-date extraction
- `TestSportsParse`: 体育学院 — dedicated parser, time/line2 extraction
- `TestScaiParse`: 计算机学院 — dedicated parser, title attr + UUID links
- `TestEveryRegisteredSiteHasAdapter`: all 70 sites and the news site have list/article hooks
- per-site adapter tests for the non-standard templates and unavailable SPA shells

Run: `go test ./internal/scraper/`

## Key Design Decisions

- **No generic "one parser fits all"**: Different sites have different HTML structures; site-specific parsers registered via `ParseFunc`
- **Domain boundary matching**: `isValidNewsURL` uses `://` prefix to avoid `scai.swjtu.edu.cn` matching `i.swjtu.edu.cn`
- **Skip patterns**: Navigation links filtered by exact match AND prefix match (e.g., "习近平给四所交通大学全体师生" matches longer titles)
- **Date extraction**: Multiple fallback strategies — list page selectors → `<time>` tag → `<div class="s2-date">` → detail page
- **Cache at HTTP level**: `FetchWithCache` wraps `fetchRaw`, all fetches go through cache

## Known Limitations

- List-page date availability depends on the individual site's HTML; adapters preserve a
  missing date for detail-page fallback rather than inventing a year.
- `--detail-date` fetches each missing date individually (slow for many items)
- SPA/login/unavailable sites (sist, xg, jwc, sro, fad, cwc, lib, journals) return explicit
  errors instead of an empty successful result
- Library search uses 维普智图 API, not the library's own search engine
- CAS-protected systems use the browser-assisted flow above; structured
  extraction still requires a per-system adapter.

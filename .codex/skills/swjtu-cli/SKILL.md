# SWJTU-CLI Skill

## Purpose

CLI tool for accessing SWJTU (西南交通大学) campus data. Scrapes news from 77 university websites and searches academic literature via the library API.

## Quick Start

```bash
cd C:\DevCodes\Others\SWJTU-CLI
go build -o swjtu-cli.exe ./cmd/swjtu-cli/
./swjtu-cli.exe news                          # Latest news from all sites
./swjtu-cli.exe news civil                    # 土木工程学院
./swjtu-cli.exe lib search "人工智能"          # Academic search
./swjtu-cli.exe sites                         # List all 77 sites
```

## Commands

| Command | Description |
|---------|-------------|
| `news` | Aggregate all sites |
| `news <category>` | jdyw/mtjd/tzgg/xshd/ybdt/sjmt |
| `news <site-id>` | Single site (e.g. civil, glxy, dqxy) |
| `news <site-id> <slug>` | Sub-category (e.g. civil tzgg) |
| `news --page N` | Page N of results |
| `news --from DATE --to DATE` | Date range filter |
| `news --detail-date` | Fetch missing dates from detail pages |
| `news --nocache` | Bypass 5-min cache |
| `read <url>` | Full article with attachments |
| `download-all <url>` | Download all attachments |
| `lib db` | Database navigation |
| `lib news` | Library news |
| `lib search <keyword>` | Academic literature search |
| `lib search <keyword> title/author` | Search by field |
| `sites [type]` | List sites (college/party/admin/unit/institute) |
| `test-all` | Verify all 77 sites |

## Architecture

```
internal/scraper/
├── boda.go         # 博达 CMS universal parser (5 patterns)
├── boda_test.go    # 9 unit tests with HTML snapshots
├── cache.go        # 5-min TTL in-memory cache
├── client.go       # HTTP client with cache
├── ese.go          # 环境学院专属解析器
├── ees.go          # 卓越工程师学院专属解析器
├── library.go      # 图书馆API (维普智图)
├── scai.go         # 计算机学院专属解析器
├── sports.go       # 体育学院专属解析器
├── sites.go        # Site registry (ParseFunc field for custom parsers)
├── agg.go          # Concurrent aggregation
└── testdata/       # HTML snapshots for tests (46 sites)
```

## Parser Types

**Generic (boda.go)**: Handles most 博达 CMS sites with 5 patterns. Used when `ParseFunc` is nil.

**Site-specific**: Registered via `ParseFunc` in `sites.go`:
- `ese.go`: s2-date/p structure + s6-tx/h3 structure
- `sports.go`: `<time>` + `<p class="line2">` layout
- `scai.go`: Vue SPA with `title` attr + UUID links
- `ees.go`: `<p class="title">` with full article in `<a>`

## Adding a New Site

1. Add entry to `AllSites` in `sites.go`
2. If standard 博达 CMS: just set Category + SubPages
3. If non-standard: write dedicated parser file, set `ParseFunc`
4. Save HTML to `testdata/` for unit testing
5. Add test to `boda_test.go`
6. Run `go test ./internal/scraper/` + `test-all`

## Cache & Performance

- HTTP responses cached 5 minutes (in-memory)
- `--nocache` flag bypasses cache
- `--detail-date` fetches each missing date from detail pages (slower)
- Cache speeds up repeated calls ~40%

## Known Limitations

- 6 sites removed (server faults): zzb, xcb, jiuye, zlfz, psy, jtaq
- 8 SPA sites return empty: sist, xg, jwc, sro, fad, cwc, lib, journals
- Many sites lack dates in list pages (46/75 show ----.---.---)
- `--detail-date` fetches individually (slow for many items)
- CAS-authenticated sites not supported

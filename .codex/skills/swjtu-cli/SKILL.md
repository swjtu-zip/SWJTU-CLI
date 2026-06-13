# SWJTU-CLI Skill

## Purpose

CLI tool for accessing SWJTU (西南交通大学) campus data. Scrapes news, notices, and academic info from 77 university websites.

## Quick Start

```bash
cd C:\DevCodes\Others\SWJTU-CLI
go build -o swjtu-cli.exe ./cmd/swjtu-cli/
./swjtu-cli.exe news              # Latest news from all sites
./swjtu-cli.exe sites             # List all 77 sites
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
| `read <url>` | Full article with attachments |
| `download-all <url>` | Download all attachments |
| `sites [type]` | List sites (college/party/admin/unit/institute) |
| `test-all` | Verify all 77 sites |

## Architecture

```
internal/scraper/
├── client.go    # HTTP client with UA spoofing
├── boda.go      # 博达CMS parser (shared by all 77 sites)
├── sites.go     # Site registry with URLs and sub-pages
└── agg.go       # Concurrent aggregation
```

All college sites use 博达 Visual SiteBuilder 9 CMS with identical HTML structure. One parser covers all.

## Adding a New Site

1. Add entry to `AllSites` in `sites.go`
2. Set Category: college/party/admin/unit/institute
3. Add SubPages if known (e.g. `[]string{"tzgg", "xyxw"}`)
4. Run `test-all` to verify

## CMS Patterns

| CMS | Sites | Pattern |
|-----|-------|---------|
| 博达 VSB 9 | 77 sites | SSR HTML, `info/{cat}/{id}.htm` links |
| VATUU | jwc, yz | AJAX, needs different parser |
| SPA | one, i, lib | Vue.js, not scrapable |

## Known Limitations

- 6 sites removed (server faults): zzb, xcb, jiuye, zlfz, psy, jtaq
- SPA sites (一网通办, 扬华智办, 图书馆) not supported
- CAS-authenticated sites (财务) not supported

# SWJTU-CLI

西南交通大学校园信息 CLI 工具。在终端中获取校内新闻、通知公告、学术活动、图书馆资源等信息。

## 安装

```bash
go build -o swjtu-cli.exe ./cmd/swjtu-cli/
```

## 使用

### 新闻

```bash
swjtu-cli news                          # 聚合所有站点最新新闻
swjtu-cli news jdyw                     # 交大要闻
swjtu-cli news tzgg                     # 通知公告
swjtu-cli news mtjd                     # 媒体交大
swjtu-cli news xshd                     # 学术活动
```

### 学院 / 部门

```bash
swjtu-cli news civil                    # 土木工程学院
swjtu-cli news civil tzgg               # 土木学院通知公告
swjtu-cli news glxy xwtz/tzgg           # 经管学院通知公告
swjtu-cli news college                  # 聚合所有学院
swjtu-cli news party                    # 聚合党群部门
swjtu-cli news admin                    # 聚合行政部门
```

### 历史数据查询

```bash
swjtu-cli news --page 2                 # 第2页
swjtu-cli news civil tzgg --from 2026-01-01 --to 2026-03-31
swjtu-cli news --from 2026-06-01        # 6月以来所有新闻
swjtu-cli news --max-pages 5            # 最多翻5页
```

### 详情页补日期

部分学院网站列表页不显示日期，用 `--detail-date` 从文章详情页抓取：

```bash
swjtu-cli news civil --detail-date      # 从详情页补全缺失日期
```

### 缓存机制

新闻数据默认缓存5分钟，用 `--nocache` 绕过：

```bash
swjtu-cli news civil                    # 使用缓存（5分钟有效）
swjtu-cli news civil --nocache          # 绕过缓存，强制刷新
```

### 图书馆

```bash
swjtu-cli lib db                        # 数据库导航
swjtu-cli lib news                      # 图书馆新闻
swjtu-cli lib search "人工智能"          # 学术文献检索（全文）
swjtu-cli lib search "水资源" title     # 按题名检索
swjtu-cli lib search "王红瑞" author    # 按作者检索
swjtu-cli lib search --page 2           # 第2页
```

### 阅读文章

```bash
swjtu-cli read <文章URL>                # 查看全文、图片、附件
```

### 下载附件

```bash
swjtu-cli download <文件URL>            # 下载单个文件
swjtu-cli download-all <文章URL>        # 下载文章全部附件(Excel/Word/PDF/图片)
```

### 统一登录与浏览器抓取

对于需要统一身份认证、但没有开放 API 的业务系统，使用独立浏览器会话：

```bash
swjtu-cli auth login https://one.swjtu.edu.cn/       # 浏览器中手动完成登录
swjtu-cli auth fetch https://one.swjtu.edu.cn/ --out page.html --wait 3
swjtu-cli auth profile                               # 查看独立会话目录
```

程序会启动专用的 Chrome/Chromium profile。密码、验证码和二次验证均由用户在浏览器中完成，CLI 不读取日常浏览器 profile，也不保存原始密码。可通过 `SWJTU_CLI_BROWSER` 指定浏览器可执行文件。

### 站点管理

```bash
swjtu-cli sites                         # 列出所有77个站点
swjtu-cli sites college                 # 只看学院
swjtu-cli test-all                      # 全量测试
```

## 支持的站点

77个二级单位网站，分为5类：

| 分类 | 数量 | 命令 |
|------|------|------|
| 教学科研单位 | 37 | `news college` |
| 党群部门 | 12 | `news party` |
| 行政部门 | 19 | `news admin` |
| 直属单位与企业 | 6 | `news unit` |
| 研究院 | 2 | `news institute` |
| 校级站点 | 6 | `news jdyw/tzgg/...` |

8个SPA/无法访问站点返回空结果：sist, xg, jwc, sro, fad, cwc, lib, journals

## 技术栈

- Go (标准库)
- goquery (HTML解析)
- chromedp (独立 Chrome/Chromium 浏览器控制)
- 博达 Visual SiteBuilder 9 CMS 适配（通用解析器 + 站点专属解析器）
- 维普智图 API（图书馆学术检索）

## 测试

```bash
go test ./internal/scraper/             # 运行解析器单元测试
swjtu-cli test-all                      # 全量站点测试
```

### Go SDK

归档服务等 Go 程序可以直接使用 `pkg/sdk`，不需要解析 CLI 的终端输出：

```go
client := sdk.New(sdk.Options{})
feeds := sdk.Feeds()
items, _ := client.ListFeed(ctx, feeds[0], 1)
article, raw, _ := client.FetchArticleSnapshot(ctx, items[0].URL)
resource, _ := client.DownloadResource(ctx, article.Images[0])
defer resource.Body.Close()
```

SDK 保留列表解析、文章正文 HTML、图片/附件 URL 和流式资源下载；现有命令行行为保持不变。

单元测试使用 `testdata/` 下保存的HTML快照，不依赖网络。

## 目录结构

```
swjtu-cli/
├── agent/                  # Agent 指令文件
│   ├── AGENTS.md           # 架构文档
│   └── broken-sites.md     # 不可达站点记录
├── cmd/swjtu-cli/          # CLI 入口
│   └── main.go
├── internal/
│   ├── scraper/            # 爬虫层
│   │   ├── agg.go          # 聚合并发抓取
│   │   ├── boda.go         # 博达CMS通用解析器
│   │   ├── boda_test.go    # 解析器单元测试
│   │   ├── cache.go        # HTTP缓存（5分钟TTL）
│   │   ├── client.go       # HTTP客户端
│   │   ├── ese.go          # 环境学院专属解析器
│   │   ├── ees.go          # 卓越工程师学院专属解析器
│   │   ├── library.go      # 图书馆API客户端
│   │     ├── scai.go       # 计算机学院专属解析器
│   │     ├── sports.go     # 体育学院专属解析器
│   │     ├── sites.go      # 站点注册表（含ParseFunc字段）
│   │     └── testdata/     # 单元测试HTML快照（46个站点）
│   ├── browser/             # 独立浏览器会话与渲染页面抓取
│   └── model/
│       └── news.go         # 数据模型
├── go.mod
├── go.sum
├── .gitignore
└── README.md
```

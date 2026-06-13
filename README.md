# SWJTU-CLI

西南交通大学校园信息 CLI 工具。在终端中获取校内新闻、通知公告、学术活动等信息。

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
| 教学科研单位 | 31 | `news college` |
| 党群部门 | 12 | `news party` |
| 行政部门 | 19 | `news admin` |
| 直属单位与企业 | 6 | `news unit` |
| 研究院 | 2 | `news institute` |
| 校级站点 | 6 | `news jdyw/tzgg/...` |

## 技术栈

- Go + cobra CLI
- goquery (HTML解析)
- 博达 Visual SiteBuilder 9 CMS 适配
- VATUU / 众望升腾 CMS 适配

## 目录结构

```
swjtu-cli/
├── agent/                  # Agent 指令文件
│   ├── AGENTS.md
│   └── broken-sites.md
├── cmd/swjtu-cli/          # CLI 入口
│   └── main.go
├── internal/
│   ├── scraper/            # 爬虫层
│   │   ├── agg.go          # 聚合并发抓取
│   │   ├── boda.go         # 博达CMS解析器
│   │   ├── client.go       # HTTP客户端
│   │   └── sites.go        # 站点注册表
│   └── model/
│       └── news.go         # 数据模型
├── go.mod
├── go.sum
├── .gitignore
└── README.md
```

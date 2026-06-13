# SWJTU-CLI — Architecture & Agent Guide

## Project Overview

Go CLI tool that helps SWJTU students access campus data via terminal. Starts with news scraping, designed to expand to other OA systems (教务, 邮件, 一网通办, etc).

**Data access method**: HTML scraping — no public JSON APIs available for campus news.

## Quick Reference

- **Broken/down sites**: `agent/broken-sites.md`
- **Parser code**: `internal/scraper/boda.go` (unified 博达 CMS parser)
- **Site registry**: `internal/scraper/sites.go` (all 31 college sites + 4 university sites)
- **Models**: `internal/model/news.go`

## Key Finding: All College Sites Use the Same CMS

SWJTU的31个二级学院/研究院网站全部使用 **博达 Visual SiteBuilder 9** CMS，HTML结构完全一致。
只需一个统一解析器 (`boda.go`) 即可覆盖所有学院网站，无需为每个学院写单独的爬虫。

## Directory Layout (actual)

```
SWJTU-CLI/
├── agent/
│   ├── AGENTS.md          # this file
│   └── broken-sites.md    # sites that are down/unsupported
├── cmd/
│   └── swjtu-cli/
│       └── main.go        # CLI entrypoint (cobra)
├── internal/
│   ├── scraper/
│   │   ├── client.go      # shared HTTP client (UA, timeout)
│   │   ├── boda.go        # unified 博达 CMS parser (all 31 colleges + news/youth/zhaosheng)
│   │   └── sites.go       # site registry (URLs for all accessible sites)
│   ├── model/
│   │   └── news.go        # NewsItem, Article structs
│   └── oa/                # future: 教务, 邮件, 一网通办 adapters
├── go.mod
└── go.sum
```

## CMS Types & Scrapability

SWJTU各站点使用不同的CMS系统，决定了爬取策略：

| CMS类型 | 爬取难度 | 代表站点 | 爬取方式 |
|---------|---------|---------|---------|
| **博达 Visual SiteBuilder 9** | ★☆☆ | news, www, youth, faculty, zhaosheng | 纯SSR HTML，直接解析DOM |
| **VATUU为途教学平台** | ★★☆ | jwc, yz | AJAX动态加载，需模拟请求 |
| **众望升腾学工系统** | ★★☆ | xg | 自研CMS，URL含加密参数 |
| **SPA (Vue.js)** | ★★★ | one, i, lib | 需headless浏览器或找API |
| **CAS认证保护** | ★★★★ | cw, 教务内部 | 需CAS登录，session管理 |

## 二级单位网站全景 (verified 2026-06-14)

### ✅ 可直接爬取 — 博达 Visual SiteBuilder 9

#### news.swjtu.edu.cn (新闻网)

**CMS**: 博达 Visual SiteBuilder 9 (CustomerNO: 77656262657232307476475d51535742000300034452)
**内容类别**:

| 名称 | 列表URL | categoryId |
|------|--------|-----------|
| 交大要闻 | /jdyw.htm | 1011 |
| 媒体交大 | /mtjd.htm | 1012 |
| 院部动态 | /ybdt.htm | (TBD) |
| 社交媒体 | /sjmt.htm | (TBD) |
| 通知公告 | /tzgg.htm | (TBD) |
| 学术活动 | /xshd.htm | (TBD) |

**分页**: 第1页 `/{slug}.htm`，第2页起 `/{slug}/{page}.htm` (倒序: page 2 = 77.htm, page 78 = 1.htm)
**文章URL**: `/info/{categoryId}/{articleId}.htm`

**列表页选择器**:
```html
<li class="flex">
  <span class="time">2026.06.13</span>
  <a href="info/1011/107565.htm" target="_blank" class="ep1" title="标题">标题</a>
</li>
```

**详情页选择器**:
- 标题: `h1.news_tit`
- 来源+日期: `.news_source span` (来源) + `.news_source .date` (日期: YYYY/MM/DD)
- 正文: `#vsb_content_501 .v_news_content`
- 作者信息: `.xxx` — 格式 `作者：xx 摄影：xx 编辑：xx`
- 上下篇: `.news_sxp li a`

#### youth.swjtu.edu.cn (新青年素质网 / 团委)

**CMS**: 博达 Visual SiteBuilder 9
**内容类别**:

| 名称 | URL |
|------|-----|
| 团情快讯 | /tqkx1.htm |
| 通知公告 | /tzgg.htm |
| 团委发文 | /twfw.htm |
| 基层团建 | /jctj.htm |
| 学生组织 | /xszz.htm |
| 第二课堂 | https://ocw.swjtu.edu.cn/ (外部平台) |

**列表页选择器** (团情快讯):
```html
<li>
  <a href="info/1281/171881.htm" target="_blank">
    <p class="line1">标题</p>
  </a>
  <span>06-05</span>
</li>
```

**通知公告列表**:
```html
<li>
  <a href="info/1191/172001.htm" target="_blank">
    <p class="line1">标题</p>
  </a>
  <span>06-10</span>
</li>
```

**注意**: 部分通知有活动结束时间 (`<div style="display:none">活动结束时间：2026/06/09</div>`)，可解析用于判断活动状态。

#### zhaosheng.swjtu.edu.cn (本科招生网)

**CMS**: 博达 Visual SiteBuilder 9
**公开数据**:
- 招生政策: `/zsxin_xi/zszc.htm` — 链接格式 `info/1047/{id}.htm`
- 通知公告: `/zsxin_xi/tzgg.htm` — 链接格式 `info/1042/{id}.htm`
- 招生计划: `https://cjcx.swjtu.edu.cn/plan/default.html` (外部)
- 录取查询: `https://cjcx.swjtu.edu.cn/lqcx.htm` (需考生号)

**列表选择器** (招生政策):
```html
<a href="info/1047/2819.htm">
  <li>
    <span>标题</span>
    <em>2026-06-01</em>
  </li>
</a>
```

**列表选择器** (通知公告):
```html
<a href="info/1042/2829.htm">
  <li>
    <div class="time"><span>06-02</span><span class="showyear">2026</span></div>
    <div class="cont">
      <h3><em>【通知公告】</em>标题</h3>
      <span>摘要...</span>
    </div>
  </li>
</a>
```

#### faculty.swjtu.edu.cn (教师个人主页)

**CMS**: 博达 Visual SiteBuilder 9 (CustomerNO: 7765626265723230747647545353554503090007)
**公开数据**:
- 教师列表: `/xylb.jsp?urltype=tree.TreeTempUrl&wbtreeid=1003` (按学院)
- 按拼音查: `/pyjslb.jsp?urltype=tsites.PinYinTeacherList&wbtreeid=1001&py=a`
- 个人主页: `/{username}/zh_CN/index.htm`

**教师主页结构**:
- 每个教师有独立子目录 (如 `/shenzhiyun1/`, `/hechuan/`)
- 主页内容在博达CMS的组件中，需解析具体页面

### ⚠️ 需模拟AJAX — VATUU为途平台

#### jwc.swjtu.edu.cn (教务处 / 本科生院)

**CMS**: VATUU为途教学信息服务平台
**公开数据 (无需登录)**:
- 新闻通知: 通过AJAX加载，URL格式 `/vatuu/WebAction?setAction=newsList`
- 学术讲座: `/vatuu/LectureAction?setAction=LectureMore`
- 教学相关: `/vatuu/WebAction?setAction=newsList&selectType=bigType&bigTypeId=FB2D32BEAD066637`
- 空闲教室: `/vatuu/CourseAction?setAction=classroomQuery`

**热点功能**:
- 课表查询 (需登录)
- 考试安排 (需登录)
- 成绩查询 (需登录)
- 四六级成绩查询 (链接到 cet.neea.edu.cn)

**注意**: VATUU平台的新闻列表通过JS函数 `indexNewsAjax()` 动态加载，需找到对应的API endpoint。

#### yz.swjtu.edu.cn (研究生招生网)

**CMS**: VATUU为途教学信息服务平台
**公开数据**:
- 通知公告: `/vatuu/WebAction?setAction=newsList`
- 硕士招生: `/vatuu/WebAction?setAction=newsList&selectType=smallType&keyWord=61E92EF67418DC54`
- 博士招生: `/vatuu/WebAction?setAction=newsList&selectType=smallType&keyWord=22251884ACC79046`
- 招生计划: `/vatuu/PlanMasterMajorAction?setAction=intro&year=...&type=zszyml`
- 复试分数线: `/vatuu/RetrialScoreAdminAction?setAction=publicRetrialMasterLine`
- 导师信息: `/vatuu/TeacherInfoAction?setAction=homepageList`

#### gsnews.swjtu.edu.cn (研究生院)

**CMS**: VATUU
**状态**: 需进一步探索

### ⚠️ 自研CMS — 众望升腾

#### xg.swjtu.edu.cn (扬华素质网 / 学工部)

**CMS**: 四川众望升腾科技有限公司
**公开数据**:
- 通知公告: `/web/Home/PushNewsList?Lmk7LJw34Jmu=010j.shtml`
- 扬华新闻: `/web/Home/NewsList?LJw34Jmu=011e.shtml`
- 多彩学院: `/web/Home/ColourfulCollegeNewsList`
- 学生活动: `/web/Home/TroupeActive`
- 公示专栏: `/web/Publicity/List`

**URL格式**: 含加密参数如 `xvw34vmu=011e_M3=...`，参数包含分类和文章ID
**详情页**: `/web/Home/Detail?xvw34vmu=010j_M3=325fc3jf-f4ca-egfg-j20b-ef0c2cf0jb1h.shtml`

**列表选择器** (通知公告):
```html
<li>
  <a href="/web/Home/Detail?xvw34vmu=..." title="标题">标题</a>
  <span class="right date">2026-06-12</span>
</li>
```

### ❌ SPA/需要认证 — 不可直接爬取

| 站点 | 域名 | 技术栈 | 说明 |
|------|------|--------|------|
| 一网通办 | one.swjtu.edu.cn | Vue.js SPA | 所有内容通过JS加载，无SSR |
| 扬华智办 | i.swjtu.edu.cn | Vue.js SPA | 同上 |
| 图书馆 | lib.swjtu.edu.cn | Vue.js SPA | 书目检索需API |
| 财务处 | cw.swjtu.edu.cn | CAS认证 | 重定向到登录页 |
| 智能财务 | cw.swjtu.edu.cn/HOME_V3 | CAS认证 | 需CAS登录 |
| 教务内部 | jiaowu.swjtu.edu.cn/vatuu | VATUU+登录 | 课表/成绩等需登录 |
| 学工系统 | xgsys.swjtu.edu.cn | CAS认证 | 需CAS登录 |

### ❌ 无法访问

| 站点 | 域名 | 状态 |
|------|------|------|
| 信息化与网络管理处 | nic.swjtu.edu.cn | 连接失败 |
| 研究生院 | gs.swjtu.edu.cn | 连接失败 |

## CAS统一认证

**CAS入口**: `cas.swjtu.edu.cn/authserver/login`
**服务URL格式**: `?service=https://目标站点/cas_login.aspx`

需要CAS认证的功能：
- 教务系统：课表、成绩、考试安排
- 财务系统：缴费、奖学金
- 学工系统：评奖评优、资助申请
- 图书馆：借阅、续借
- VPN：校外访问校内资源

**实现策略**: 先实现无需认证的公开数据爬取，CAS认证作为后续版本功能。

## 博达 Visual SiteBuilder 9 通用规则

SWJTU大部分站点使用博达CMS (CustomerNO以 `7765626265723230747647` 开头)，有以下共同特征：

- **编码**: UTF-8, `lang="zh-cn"`
- **静态资源**: 图片路径 `/__local/{hash1}/{hash2}/{hash}/{filename}`
- **站点追踪JS**: `_jsq_(pageType, page, id, siteId)` — 可忽略
- **搜索功能**: 基于Vue的搜索组件，使用 `/aop_component/` API
- **文章结构**: `h1.news_tit` + `.news_source` + `#vsb_content_501`
- **列表结构**: `<li class="flex"><span class="time">date</span><a>title</a></li>`

## VATUU为途平台通用规则

- **新闻列表**: 通过AJAX请求 `/vatuu/WebAction?setAction=newsList` 获取
- **分类ID**: 使用hex编码的ID (如 `FB2D32BEAD066637`)
- **登录页**: `/service/login.jsp`
- **功能入口**: `/vatuu/{Action}?setAction={actionName}`

## Go Dependencies (suggested)

- CLI framework: `github.com/spf13/cobra`
- HTML parsing: `github.com/PuerkitoBio/goquery` (jQuery-like DOM traversal)
- HTTP client: stdlib `net/http`
- Table output: `github.com/jedib0t/go-pretty/v6`
- Color output: `github.com/fatih/color`

## Key Constraints

- All scraping must respect `robots.txt` (check before crawling)
- Add reasonable `User-Agent` and rate limiting — don't hammer the server
- Campus network may be required for some OA resources (news is public)
- Future OA systems (教务, 邮件) will likely need CAS authentication via `cas.swjtu.edu.cn`
- VATUU平台的AJAX请求可能需要特定的Referer和Cookie
- 众望升腾系统的URL参数可能有时效性

## CLI Commands (planned v0.1)

```
swjtu news list [category] [--page N]     # 列出最新新闻
swjtu news read <url|id>                   # 阅读全文
swjtu news search <keyword>                # 搜索新闻
swjtu youth list [category]                # 团委通知
swjtu zhaosheng list                       # 招生信息
swjtu faculty search <name>                # 查找教师
swjtu college list                         # 列出所有学院
swjtu college news <college> [category]    # 查看学院新闻
```

## All Accessible College Sites (verified 2026-06-14)

All 31 sites below use 博达 CMS, parsed by `internal/scraper/boda.go`.
See `internal/scraper/sites.go` for the complete registry with URLs.

| ID | 学院 | 域名 |
|----|------|------|
| civil | 土木工程学院 | civil.swjtu.edu.cn |
| sme | 机械工程学院 | sme.swjtu.edu.cn |
| dqxy | 电气工程学院 | dqxy.swjtu.edu.cn |
| sist | 信息科学与技术学院 | sist.swjtu.edu.cn |
| scai | 计算机与人工智能学院 | scai.swjtu.edu.cn |
| sic | 集成电路科学与工程学院 | sic.swjtu.edu.cn |
| glxy | 经济管理学院 | glxy.swjtu.edu.cn |
| sfl | 外国语学院 | sfl.swjtu.edu.cn |
| ctt | 交通运输与物流学院 | ctt.swjtu.edu.cn |
| clxy | 材料科学与工程学院 | clxy.swjtu.edu.cn |
| gsee | 地球科学与工程学院 | gsee.swjtu.edu.cn |
| ese | 环境科学与工程学院 | ese.swjtu.edu.cn |
| jzxy | 建筑学院 | jzxy.swjtu.edu.cn |
| design | 设计艺术学院 | design.swjtu.edu.cn |
| phys | 物理科学与技术学院 | phys.swjtu.edu.cn |
| rwxy | 人文学院 | rwxy.swjtu.edu.cn |
| gg | 公共管理学院 | gg.swjtu.edu.cn |
| cm | 医学院 | cm.swjtu.edu.cn |
| bioeng | 生命科学与工程学院 | bioeng.swjtu.edu.cn |
| chem | 化学学院 | chem.swjtu.edu.cn |
| lixue | 力学与航空航天学院 | lixue.swjtu.edu.cn |
| maths | 数学学院 | maths.swjtu.edu.cn |
| zzxy | 马克思主义学院 | zzxy.swjtu.edu.cn |
| sports | 体育学院 | sports.swjtu.edu.cn |
| xinli | 心理研究与咨询中心 | xinli.swjtu.edu.cn |
| leeds | 利兹学院 | leeds.swjtu.edu.cn |
| mys | 茅以升学院 | mys.swjtu.edu.cn |
| iscit | 智慧城市与交通学院 | iscit.swjtu.edu.cn |
| ees | 国家卓越工程师学院 | ees.swjtu.edu.cn |
| rvl | 轨道交通运载系统全国重点实验室 | rvl.swjtu.edu.cn |
| ai | 人工智能研究院 | ai.swjtu.edu.cn |

## College News URL Patterns

每个学院网站的新闻页面URL模式略有不同，但HTML结构一致。
以下是各学院首页的典型新闻链接：

- **学院新闻**: `/xyxw.htm`, `/xydt.htm`, `/news.htm` (各学院不同)
- **通知公告**: `/tzgg.htm`, `/notice.htm`
- **首页轮播/要闻**: 直接在 `index.htm` 或首页中

解析器会自动尝试多种选择器模式来适配不同学院的页面结构。

package scraper

// BodaSite represents a site built on 博达 Visual SiteBuilder 9 CMS.
type BodaSite struct {
	Name      string
	BaseURL   string
	Category  string   // "college", "party", "admin", "unit", "institute"
	SubPages  []string // known sub-page slugs (e.g. "tzgg", "xyxw")
}

// AllSites contains every accessible SWJTU secondary unit website.
var AllSites = map[string]*BodaSite{
	// === 教学科研单位 ===
	"civil":   {Name: "土木工程学院", BaseURL: "http://civil.swjtu.edu.cn", Category: "college", SubPages: []string{"tzgg"}},
	"sme":     {Name: "机械工程学院", BaseURL: "http://sme.swjtu.edu.cn", Category: "college", SubPages: []string{"xwtz/xyxw", "xwtz/xshd", "xsgz/tzgg", "kxyj/tzgg"}},
	"dqxy":    {Name: "电气工程学院", BaseURL: "http://dqxy.swjtu.edu.cn", Category: "college", SubPages: []string{"xwdt/qb", "xsgz_more/tzgg"}},
	"sist":    {Name: "信息科学与技术学院", BaseURL: "http://sist.swjtu.edu.cn", Category: "college"},
	"scai":    {Name: "计算机与人工智能学院", BaseURL: "https://scai.swjtu.edu.cn", Category: "college"},
	"sic":     {Name: "集成电路科学与工程学院", BaseURL: "https://sic.swjtu.edu.cn", Category: "college"},
	"glxy":    {Name: "经济管理学院", BaseURL: "http://glxy.swjtu.edu.cn", Category: "college", SubPages: []string{"xwtz/xyxw", "xwtz/tzgg", "xwtz/xsdt", "xwtz/tzgg/dzbg", "xwtz/tzgg/bks", "xwtz/tzgg/yjs", "xwtz/tzgg/xsgz", "xwtz/tzgg/kygz"}},
	"sfl":     {Name: "外国语学院", BaseURL: "http://sfl.swjtu.edu.cn", Category: "college"},
	"ctt":     {Name: "交通运输与物流学院", BaseURL: "http://ctt.swjtu.edu.cn", Category: "college", SubPages: []string{"tzgg/xytz", "xyywlist/jyyw", "bkjxnew/jxgg", "yjsjxnew/tzgg"}},
	"clxy":    {Name: "材料科学与工程学院", BaseURL: "http://clxy.swjtu.edu.cn", Category: "college", SubPages: []string{"sylm_xwgg/xyxw", "sylm_xwgg/xygg", "sylm_djgz", "sylm_xsgz", "sylm_xshd", "jwgg"}},
	"gsee":    {Name: "地球科学与工程学院", BaseURL: "http://gsee.swjtu.edu.cn", Category: "college"},
	"ese":     {Name: "环境科学与工程学院", BaseURL: "https://ese.swjtu.edu.cn", Category: "college"},
	"jzxy":    {Name: "建筑学院", BaseURL: "http://jzxy.swjtu.edu.cn", Category: "college"},
	"design":  {Name: "设计艺术学院", BaseURL: "https://design.swjtu.edu.cn", Category: "college"},
	"phys":    {Name: "物理科学与技术学院", BaseURL: "http://phys.swjtu.edu.cn", Category: "college"},
	"rwxy":    {Name: "人文学院", BaseURL: "http://rwxy.swjtu.edu.cn", Category: "college", SubPages: []string{"xyxw", "dzbg", "xshd", "xsgz/tzgg"}},
	"gg":      {Name: "公共管理学院", BaseURL: "http://gg.swjtu.edu.cn", Category: "college"},
	"cm":      {Name: "医学院", BaseURL: "http://cm.swjtu.edu.cn", Category: "college"},
	"bioeng":  {Name: "生命科学与工程学院", BaseURL: "http://bioeng.swjtu.edu.cn", Category: "college"},
	"chem":    {Name: "化学学院", BaseURL: "http://chem.swjtu.edu.cn", Category: "college"},
	"lixue":   {Name: "力学与航空航天学院", BaseURL: "http://lixue.swjtu.edu.cn", Category: "college"},
	"maths":   {Name: "数学学院", BaseURL: "http://maths.swjtu.edu.cn", Category: "college", SubPages: []string{"xyxw", "xygg", "xsgz/xgtz", "dwjw/djtz"}},
	"zzxy":    {Name: "马克思主义学院", BaseURL: "http://zzxy.swjtu.edu.cn", Category: "college", SubPages: []string{"xyyw", "tzgg", "xwdt"}},
	"sports":  {Name: "体育学院", BaseURL: "http://sports.swjtu.edu.cn", Category: "college", SubPages: []string{"xwdt", "tzggnew/qb"}},
	"xinli":   {Name: "心理研究与咨询中心", BaseURL: "http://xinli.swjtu.edu.cn", Category: "college"},
	"leeds":   {Name: "利兹学院", BaseURL: "http://leeds.swjtu.edu.cn", Category: "college"},
	"mys":     {Name: "茅以升学院", BaseURL: "http://mys.swjtu.edu.cn", Category: "college"},
	"iscit":   {Name: "智慧城市与交通学院", BaseURL: "https://iscit.swjtu.edu.cn", Category: "college"},
	"ees":     {Name: "国家卓越工程师学院", BaseURL: "https://ees.swjtu.edu.cn", Category: "college"},
	"rvl":     {Name: "轨道交通运载系统全国重点实验室", BaseURL: "http://rvl.swjtu.edu.cn", Category: "college"},
	"ai":      {Name: "人工智能研究院", BaseURL: "https://ai.swjtu.edu.cn", Category: "college"},

	// === 党群部门 ===
	"db":      {Name: "党委办公室", BaseURL: "https://db.swjtu.edu.cn", Category: "party"},
	"jw":      {Name: "纪律检查委员会办公室", BaseURL: "https://jw.swjtu.edu.cn", Category: "party"},
	"dwtzb":   {Name: "党委统战部", BaseURL: "https://dwtzb.swjtu.edu.cn", Category: "party"},
	"xuncha":  {Name: "党委巡察工作办公室", BaseURL: "https://xuncha.swjtu.edu.cn", Category: "party"},
	"talent":  {Name: "人才工作办公室", BaseURL: "https://talent.swjtu.edu.cn", Category: "party"},
	"fdc":     {Name: "党委教师工作部", BaseURL: "https://fdc.swjtu.edu.cn", Category: "party"},
	"xg":      {Name: "党委学生工作部", BaseURL: "https://xg.swjtu.edu.cn/web", Category: "party"},
	"ygb":     {Name: "党委研究生工作部", BaseURL: "https://ygb.swjtu.edu.cn", Category: "party"},
	"bwc":     {Name: "党委保卫部", BaseURL: "https://bwc.swjtu.edu.cn", Category: "party"},
	"xgh":     {Name: "校工会", BaseURL: "https://xgh.swjtu.edu.cn", Category: "party"},
	"jgdw":    {Name: "机关与直属单位党委", BaseURL: "https://jgdw.swjtu.edu.cn", Category: "party"},

	// === 行政部门 ===
	"xb":      {Name: "校长办公室", BaseURL: "https://xb.swjtu.edu.cn", Category: "admin"},
	"jwc":     {Name: "本科生院", BaseURL: "https://jwc.swjtu.edu.cn", Category: "admin"},
	"gsnews":  {Name: "研究生院", BaseURL: "https://gsnews.swjtu.edu.cn", Category: "admin"},
	"sro":     {Name: "科学技术发展研究院", BaseURL: "http://sro.swjtu.edu.cn", Category: "admin"},
	"wkjsc":   {Name: "文科发展建设处", BaseURL: "https://wkjsc.swjtu.edu.cn", Category: "admin"},
	"rsc":     {Name: "人力资源部", BaseURL: "https://rsc.swjtu.edu.cn", Category: "admin"},
	"hzc":     {Name: "合作发展处", BaseURL: "https://hzc.swjtu.edu.cn", Category: "admin"},
	"xyb":     {Name: "校友工作办公室", BaseURL: "https://xyb.swjtu.edu.cn", Category: "admin"},
	"fad":     {Name: "国际合作与交流处", BaseURL: "http://fad.swjtu.edu.cn", Category: "admin"},
	"cwc":     {Name: "计划财务处", BaseURL: "https://cwc.swjtu.edu.cn", Category: "admin"},
	"oalm":    {Name: "国有资产与实验室管理处", BaseURL: "https://oalm.swjtu.edu.cn", Category: "admin"},
	"inc":     {Name: "信息化与网络管理处", BaseURL: "https://inc.swjtu.edu.cn", Category: "admin"},
	"jjgh":    {Name: "基建规划与校园管理处", BaseURL: "https://jjgh.swjtu.edu.cn", Category: "admin"},
	"hbb":     {Name: "后勤保障部", BaseURL: "https://hbb.swjtu.edu.cn", Category: "admin"},
	"sj":      {Name: "审计处", BaseURL: "https://sj.swjtu.edu.cn", Category: "admin"},
	"ltc":     {Name: "离退休工作处", BaseURL: "https://ltc.swjtu.edu.cn", Category: "admin"},
	"em":      {Name: "峨眉校区管委会", BaseURL: "https://em.swjtu.edu.cn", Category: "admin"},
	"ic":      {Name: "东部（国际）校区管委会", BaseURL: "https://ic.swjtu.edu.cn", Category: "admin"},
	"ott":     {Name: "技术转移研究院", BaseURL: "https://ott.swjtu.edu.cn", Category: "admin"},

	// === 直属单位与企业 ===
	"lib":     {Name: "图书馆", BaseURL: "http://www.lib.swjtu.edu.cn", Category: "unit"},
	"journals":{Name: "期刊社", BaseURL: "http://journals.swjtu.edu.cn", Category: "unit"},
	"dag":     {Name: "档案馆", BaseURL: "http://dag.swjtu.edu.cn", Category: "unit"},
	"sie":     {Name: "国际教育学院", BaseURL: "https://sie.swjtu.edu.cn", Category: "unit"},
	"hospital":{Name: "校医院", BaseURL: "https://hospital.swjtu.edu.cn", Category: "unit"},
	"kfjt":    {Name: "科技发展集团", BaseURL: "http://kfjt.swjtu.edu.cn", Category: "unit"},

	// === 研究院 ===
	"xczx":    {Name: "乡村振兴与文化旅游发展研究院", BaseURL: "https://xczx.swjtu.edu.cn", Category: "institute"},
	"scyshj":  {Name: "四川省营商环境研究院", BaseURL: "https://scyshj.swjtu.edu.cn", Category: "institute"},
}

// NewsSite is the main university news site.
var NewsSite = &BodaSite{
	Name:    "西南交通大学新闻网",
	BaseURL: "https://news.swjtu.edu.cn",
}

// NewsCategories maps news site slugs to human-readable category names.
var NewsCategories = map[string]string{
	"jdyw": "交大要闻",
	"mtjd": "媒体交大",
	"ybdt": "院部动态",
	"sjmt": "社交媒体",
	"tzgg": "通知公告",
	"xshd": "学术活动",
}

// CategoryNames maps category codes to display names.
var CategoryNames = map[string]string{
	"college":   "教学科研单位",
	"party":     "党群部门",
	"admin":     "行政部门",
	"unit":      "直属单位与企业",
	"institute": "研究院",
}

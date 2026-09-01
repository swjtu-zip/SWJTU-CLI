# 已移除的站点 (服务器故障)

以下站点因服务器本身问题已从注册表中移除：

| 站点 | 域名 | 状态 |
|------|------|------|
| 党委组织部 | zzb.swjtu.edu.cn | 502 |
| 党委宣传部 | xcb.swjtu.edu.cn | 404 |
| 大学生就业指导中心 | jiuye.swjtu.edu.cn | TLS握手失败 |
| 发展规划与学科建设处 | zlfz.swjtu.edu.cn | 404 |
| 应用心理学研究院 | psy.swjtu.edu.cn | DNS不存在 |
| 交通安全技术研究院 | jtaq.swjtu.edu.cn | DNS不存在 |

## 动态/不可直接采集站点 (返回明确错误)

以下站点仍注册在 `sites.go` 中，但当前列表不可由静态 HTTP 页面直接取得；对应 adapter
会返回明确错误，而不是返回成功的空结果：

| 站点 | 域名 | 技术栈 |
|------|------|--------|
| 信息科学与技术学院 | sist.swjtu.edu.cn | SPA |
| 党委学生工作部 | xg.swjtu.edu.cn | 众望升腾 CMS + 认证 |
| 本科生院 | jwc.swjtu.edu.cn | VATUU + 认证 |
| 科学技术发展研究院 | sro.swjtu.edu.cn | SPA |
| 国际合作与交流处 | fad.swjtu.edu.cn | SPA |
| 计划财务处 | cwc.swjtu.edu.cn | SPA |
| 图书馆 | lib.swjtu.edu.cn | SPA (有独立API) |
| 期刊社 | journals.swjtu.edu.cn | SPA |

东部（国际）校区管委会 `ic.swjtu.edu.cn` 当前返回 HTTP 403，保留在站点注册表中，
但实网审计会报告可诊断的访问错误。

## CAS认证保护

| 站点 | 域名 | 说明 |
|------|------|------|
| 智能财务 | cw.swjtu.edu.cn | 重定向到登录页 |

## 占位符页面 (javaScript:;)

信息化研究院、智能控制与仿真工程研究中心、智能检测研究院、网络空间安全研究院

# Changelog

本文件记录 WireMesh 控制平面（后端 + 前端控制台）的发布历史。
版本号规则：`v主.次.修订`；Agent 二进制有独立版本线（当前 `0.3.7`，
见 `cmd/wiremesh-agent/main.go`），Docker 构建默认值与其一致。

## v0.7.13（README 重构为 GitHub 标准格式）

- README 按 GitHub 流行规范重构：徽章区（Go/Vue/Vite/CI/License）、
  特性清单、截图占位、目录 TOC、快速开始、数据库、Docker 部署、
  Agent 接入、自动定位、环境变量参考表、安全、升级、架构图、
  开发与 CI、FAQ、贡献与许可
- 保留全部既有技术内容（安全边界、升级指引、GeoIP、SSO 等），
  重组为清晰分节并补充环境变量速查表与 FAQ

## v0.7.12（P2 安全专项：其余 Low 项）

0day 审计 P2 收尾：

- **安装脚本加固**：换行校验覆盖全部参数（INTERFACES/间隔/TOKEN），
  escape_env 增加 `$` 转义（防 agent.env 被 source 时注入）
- **Cookie Secure**：新增 `WIREMESH_COOKIE_SECURE` 配置，反代终结
  TLS 的部署可显式开启（防明文通道携带会话 Cookie）
- **安全响应头**：全局 X-Content-Type-Options/X-Frame-Options/
  Referrer-Policy
- MFA 枚举保持现状（otp_required 仅在密码验证通过后返回，已是最小暴露）
- go vet/go test 全绿

## v0.7.11（P2 安全专项 L-1 + L-3：viewer 私钥导出 + Agent 默认验签）

0day 审计 P2 修复：

- **L-1**：`client-config`/`peer-config`（含 WireGuard 私钥/PresharedKey）
  从 viewer 提升为 operator 级——viewer 不再能导出任意节点私钥
- **L-3**：Agent 自更新 fail-closed——未配置 `--update-public-key` 时
  拒绝执行 `update_agent`（防纯 HTTP 下更新包被 MITM 替换为 root
  恶意二进制）；需要远程更新的部署必须配置公钥或手动重装
- 新增 L-1 专项测试
- go vet/go test 全绿

## v0.7.10（P2 安全专项 M-7 + M-8：MFA 密码复核 + 改密吊销会话）

0day 审计 P2 修复：

- **M-7**：`mfaSetup`/`mfaEnable` 要求当前密码复核——会话劫持者无法
  再零验证轮换 MFA 秘密接管账号（与 mfaDisable 双重复核对齐）
- **M-8**：修改密码后吊销该用户其它会话令牌（当前会话保留）——已窃取
  的其它会话不再有效，缩小令牌重放窗口
- 前端 MFA 设置/启用流程收集密码
- 新增 M-7/M-8 专项测试
- go vet/go test/npm run build 全绿

## v0.7.9（P2 安全专项 M-5：身份缺失 fail-closed）

0day 审计 P2 修复：

- Agent 证书指纹校验：身份记录缺失（errNotFound）时改为拒绝
  （此前跳过校验放行）——证书未登记或身份被删（恢复备份/手工删表）
  场景下已吊销证书不再"复活"
- 新增 M-5 专项测试
- go vet/go test 全绿

## v0.7.8（P2 安全专项 M-4：X-Agent-ID 信任收紧）

0day 审计 P2 修复：

- `WIREMESH_TRUST_PROXY_AGENT_ID` 模式下，无 mTLS 证书、依赖
  X-Agent-ID 头的请求，直连来源必须是私网/回环（可信反代所在）；
  公网直连即使伪造头也拒绝——防后端暴露被冒充节点窃取私钥
- 更新 s2 测试（私网放行 / 公网拒绝）
- go vet/go test 全绿

## v0.7.7（P1 安全专项 M-6：logout 吊销重启失效）

0day 审计 P1 修复（P1 全部完成）：

- logout 的吊销记录现在携带租户 ID（从令牌解析），重启后
  loadRevokedTokens 不再跳过该行——登出令牌不会"复活"至原 TTL
- 新增 M-6 专项测试（logout 带租户吊销、重启后仍拒绝）
- go vet/go test 全绿

## v0.7.6（P1 安全专项 M-3：停用/降级级联删除 API 令牌）

0day 审计 P1 修复：

- 用户被停用**或降级不再具备管理员角色**时，级联删除其创建的 API
  令牌（API 令牌恒为 admin 权限，降级/停用后必须失效，杜绝长期后门）
- 新增 M-3 专项测试（降级删令牌、停用删令牌）
- go vet/go test 全绿

## v0.7.5（P1 安全专项 M-1 + M-2：数据库向导加固）

0day 审计 P1 修复：

- **M-1 探测 oracle**：`configureDatabase` 失败不再回显驱动级错误
  （dial/连接拒绝/主机名等），统一通用文案；细节只进服务端日志
  （脱敏），与 testDatabase 一致，封堵内网探测
- **M-2 DNS rebinding**：远程数据库主机在校验时解析为安全 IP 并替换
  进 DSN，连接阶段不再重新解析（与通知/OIDC 外呼的单次解析一致），
  封堵校验与连接之间的重绑定窗口
- 新增 M-1/M-2 专项测试
- go vet/go test 全绿

## v0.7.4（P1 安全专项 H-3：SSO 授权码劫持修复）

0day 审计 P1 首项修复（High）：

- **PKCE**：SSO 授权请求携带 `code_challenge`（S256），回调兑换携带
  `code_verifier`——即使授权码被截获，无 verifier 也无法兑换令牌
- **固定 redirect_uri 源**：新增 `WIREMESH_PUBLIC_URL` 配置，配置后
  SSO 回调地址使用固定公网源，不再信任攻击者可控制的 Host 头；
  未配置时保留严格 Host 校验（S7）+ PKCE 兜底
- 新增 H-3 专项测试（固定源优先、伪造 Host 不影响、PKCE 兑换）
- go vet/go test 全绿

## v0.7.3（P0 安全专项 H-4：未认证初始化接管）

0day 审计 P0 第四项修复（High，P0 全部完成）：

- `WIREMESH_SETUP_TOKEN` 未配置时，服务端首次启动自动生成 256 位
  随机初始化口令：打印到日志并写入 `wiremesh-setup-token`（0600），
  初始化向导必须携带 `X-Setup-Token`——全新实例不再可被未认证抢占
- docker-compose 示例补充口令说明（支持显式注入）
- 新增 H-4 专项测试（口令熵/唯一性、错误/缺失口令 401、正确放行）
- go vet/go test 全绿

## v0.7.2（P0 安全专项 H-2：enroll 端点 TLS 守卫）

0day 审计 P0 第三项修复（High）：

- `POST /agent/v1/enroll` 补上 TLS fail-closed 守卫：纯 HTTP（未显式
  开启开发开关/可信反代）时拒绝注册——此前该端点返回节点 mTLS 私钥
  +证书却无任何 TLS 检查，MITM 可窃取注册令牌与私钥永久冒充节点
- 新增 H-2 专项测试（纯 HTTP 拒绝、开发开关放行）
- go vet/go test 全绿

## v0.7.1（P0 安全专项 H-1：心跳标签覆写 → 访问策略绕过/拓扑自授）

0day 审计 P0 第二项修复（High）：

- Agent 心跳不再接受标签覆写：`wiremesh.role`/`wiremesh.relay` 等
  拓扑角色标签与自定义标签（访问策略 source_label 依据）只能由控制台
  operator/admin 维护，任意节点无法再自授 hub 角色或伪造 team=ops
  绕过访问策略
- 注册（enroll）时剥离 Agent 自报的 `wiremesh.*` 保留前缀管理标签
- 新增 H-1 专项测试（心跳不可覆写标签、注册剥离保留标签）
- go vet/go test 全绿

## v0.7.0（P0 安全专项 C-1：备份/恢复跨租户越权）

0day 审计 P0 首项修复（Critical）：

- **平台绑定标记**：备份前在库内写入 `backup_meta`（instance_id +
  master-key 派生 HMAC）；恢复前校验标记，跨实例备份一律拒绝
  （防任意租户 admin 用另一实例备份注入/覆盖全库）
- **恢复二次认证**：`POST /api/v1/settings/backup/restore` 增加
  当前密码 +（启用 MFA 时）动态验证码复核
- **恢复后清空全部内存会话**：库被整体替换，内存会话/吊销表与磁盘
  不一致，强制所有用户重新登录（含操作者）
- 新增 C-1 专项测试（跨实例拒绝、同实例成功、恢复后会话清空、
  恢复缺密码 401）

## v0.6.1-fix（恢复地图线段实现）

- 撤销 v0.6.1 对 WorldMap.vue 的连线改动（curveCoords 跨 180° 短路径
  与 wrapX:false），恢复为 v0.6.0 的原始实现——原方案在部分数据/缩放
  场景下引入新问题，按反馈回退，等待后续以更稳妥的方式重新处理
- 按钮样式（btn-secondary 补全）与 DNS/访问策略页顶部对齐保留

## v0.6.1（按钮样式 / 布局对齐 / 地图连线修复）

- 补全 `btn-secondary` 样式定义：此前未定义导致 33 处按钮渲染成
  无边框原生按钮（看起来像描述文本）；现在带边框、深色底、悬停高亮
- `btn-primary` 增加边框，与整体按钮风格统一
- DNS 管理页 / 访问策略页：顶部说明文字与网络选择器改为顶部对齐
  （`items-start` + 与选择器同基线），不再与底部错位
- 地图连线修复：矢量图层 `wrapX:false`，避免长距离连线在低缩放时
  因多世界副本渲染只显示一半；跨 180° 经线的曲线改走短路径并回绕
  进单一世界范围，缩放后完整显示

## v0.6.0（UX 优化 + Agent 公网 IPv4 修复）

Agent 0.3.7：

- 修复 Agent 公网 IP 上报时序 bug：`agentAPI` 在 PublicIP 赋值前创建，
  持有的是 state 值副本，导致 `X-Agent-Public-IP` 头始终为空、服务端
  回退到连接源地址（双栈主机取 IPv6），GeoIP 定位与节点真实 IPv4
  不一致。现在 PublicIP 赋值后无条件重建 agentAPI
- 服务端 `requestPublicIP`：连接源地址改为 IPv4 优先于 IPv6（双栈主机
  与节点 IPv4 端点/GeoIP 一致）；Agent 自报 IPv6 也接受（无 IPv4 时）

控制台 UX：

- GeoIP 重载进度条（重载期间 0→100% 动画反馈）
- 「接入新节点」默认不强制 mTLS（HTTP/HTTPS 开箱即用）；系统设置新增
  「接入命令默认启用 mTLS」开关，开启时生成命令带 `--mtls` 并提示已
  部署客户端需重新生成接入命令更新
- 接入弹窗安装选项改为默认折叠，减少首屏杂乱
- 首页 GeoIP 未配置一次性警告（localStorage 记忆关闭、点击跳转
  GeoIP 设置、配置后消失）
- 访问策略页：资源/策略表单从列表下方改为居中弹窗，列表卡片右上角
  「添加资源/添加策略」，空状态提示更清晰
- 临时对等端表格：列宽固定 + 长内容省略，IPv6 公网端点压缩显示
  （保留头尾中间省略，hover 显示完整）

## v0.5.8-fix2（安装脚本：公钥文件引用条件化）

- 修复 Agent 启动失败重启循环：未配置签名公钥时，ExecStart 不再无条件
  引用不存在的 `/etc/wiremesh-agent/update-public-key.pem`（改为
  `${UPDATE_PUBLIC_KEY_ARGS}` 条件展开，仅公钥非空时追加参数）
- service 段 heredoc 从 `<<'EOF'` 改为 `<<EOF` 并转义 `${WIREMESH_*}`，
  使条件变量能正确展开

## v0.5.8-fix（安装脚本引号与公钥注入修复）

- 修复 `USE_MTLS="'false'"` 双重引号 bug：模板自带双引号，替换值改为
  裸值，避免嵌套引号导致 mTLS 自动判断失效
- 更新签名公钥改为 base64 单行注入：PEM 多行文本直接嵌入 env 会破坏
  shell 语法，现以 base64 存储、脚本内解码写独立公钥文件；Agent 新增
  `--update-public-key-file` 读取该文件
- 前端生成的 curl 命令 URL 加单引号包裹，修复 `&` 查询参数被 bash
  后台化导致脚本未执行的问题

## v0.5.8（Agent 接入开关可配置化 + 文档同步）

- 控制台「接入新节点」弹窗新增安装选项开关，按部署环境预置生成命令：
  - mTLS：默认 HTTPS 开 / HTTP 关，可手动切换；一键脚本与手动命令同步
  - 更新签名校验：开启后一键脚本由服务端自动注入更新签名公钥，
    Agent 强制校验更新清单签名；服务端未配置签名密钥时自动忽略
- 安装脚本支持查询参数预置：`mtls=true|false`、`update_public_key=true`；
  脚本运行时仍可 `--mtls/--no-mtls/--update-public-key` 覆盖
- 服务端导出更新签名公钥（`updateSigningPublicKeyPEM`）供脚本内嵌
- README 新增「Agent onboarding options」小节，同步安装开关说明
- 新增安装脚本选项专项测试（默认跟随协议 / mtls 参数 / 公钥内嵌）

## v0.5.7-fix（CI 构建修复：GeoIP 数据库不再内置镜像）

- 移除 Dockerfile 中 `COPY GeoLite2-City.mmdb`：该文件受 MaxMind 许可
  约束被 `.gitignore` 排除、不在 git 仓库，CI 从源码构建时上下文无此
  文件导致 `docker build` 失败
- GeoIP 改为运行时 volume 挂载（可选）：镜像默认
  `WIREMESH_GEOIP_DB=/data/GeoLite2-City.mmdb`，由部署方挂载
  `./data/GeoLite2-City.mmdb:/data/GeoLite2-City.mmdb:ro`；
  未挂载时节点优雅回退到 Agent 上报坐标（loadGeoIP 失败仅降级，不崩溃）
- README / docker-compose 示例同步说明

## v0.5.7（文档/部署专项）

- README 新增「Upgrading」章节：备份优先、master key 保持不变的加密
  兼容性说明、数据库配置持久化、Agent 无需重装（证书续期 + 自更新）、
  Go 工具链最低版本要求、降级注意
- Dockerfile：Go 构建镜像 1.26.2 → 1.26.6（与 go.mod 最低版本一致，
  修复标准库漏洞）；Agent 版本线注释对齐
- docker-compose 示例镜像标签更新为 0.5.x 并注明升级时 master key 不变
- .dockerignore / .gitignore 补全敏感文件排除（wiremesh-ca/database/
  master key、*.pem/*.key、临时数据库文件）

## v0.5.6（依赖与供应链专项）

- Go 工具链升级至 1.26.6：修复 7 个标准库漏洞（net/url、crypto/tls、
  net/http、encoding/xml、encoding/asn1、html/template），govulncheck
  从 7 个受影响降至 0
- `go.mod` 声明 go 1.26.6，CI setup-go 固定该版本（构建工具链不得低于
  漏洞修复版本）；`go mod tidy` 清理 31 行冗余 go.sum 条目
- 前端依赖升级至 wanted 版本（ol/pinia/vue/vue-router/vite/vue-tsc），
  npm audit 保持 0 漏洞
- 唯一残留提示：x/crypto 的 openpgp 包不受维护（代码未使用 openpgp，
  仅 bcrypt/argon2），模块级告警可忽略

## v0.5.5（可观测性专项）

- 日志脱敏：数据库连接失败与打开数据库的错误日志统一经
  `RedactCredentials` 处理，遮蔽 URL 风格（user:password@）与键值风格
  （password=/passwd=/pwd=）凭据，防止驱动错误回显 DSN 导致密码泄漏
- metrics 端点增强：补充 Go 进程指标（goroutines/堆内存/启动时间）与
  认证状态聚合指标（活动会话/吊销令牌数），仍只暴露跨租户聚合，不含
  节点名、地址或租户信息
- 审计覆盖确认：58 处关键操作（登录/改密/用户/节点/网络/策略/DNS/告警/
  通知/SSO/MFA/备份恢复/证书续期等）均已记录审计事件

## v0.5.4（前端体验/可访问性专项）

- 可访问性（WCAG）：icon-only 按钮补 `aria-label`（退出登录、汉堡菜单、
  Peer 添加、复制、刷新、关闭等）；对话框补 `aria-label`（BaseModal/
  ConfirmDialog/EditNodeConfigModal/PeerConfigEditorModal）；侧边栏
  `aria-label`；toast 容器 `role="status" aria-live="polite"`
- 键盘可达性：确认对话框打开时自动聚焦确认按钮（焦点不滞留背景页）；
  移动端抽屉支持 Esc 关闭
- 纯装饰 SVG 标记 `aria-hidden="true"`，避免屏幕阅读器朗读图标路径

## v0.5.3（Agent 侧专项）

- Agent 错误分类：服务端 401/403（节点删除/证书吊销/身份失配）与 423
  （节点停用）识别为终止错误，心跳/配置探测/命令轮询给出明确诊断并
  降频到 1 分钟一次，避免无效重试的请求风暴；网络错误与 4xx/5xx 保持
  原有重试自愈
- 既有健壮性确认：WireGuard 配置失败原子回滚（applied/rolled_back）、
  Peer 配置先校验后替换、位置发现 30 分钟节流降级、公共 IP 启动时单次
  发现并校验公网地址、状态持久化失败不阻塞
- 新增 Agent 韧性专项测试（终止/瞬态错误分类）

## v0.5.2（三驱动兼容性专项）

- MySQL 命令领取改用 `FOR UPDATE SKIP LOCKED`（MySQL 8.0.1+），
  多控制平面实例并发时不再互相阻塞（PostgreSQL 原本已用 SKIP LOCKED）
- 新增三驱动兼容性专项测试：占位符转换规则（sqlite/mysql 保留 `?`、
  postgres 转 `$n`）、schema 语句双定义表集合一致性、MySQL 主键类型
  约束、upsert 分派正确性、窗口函数保留策略 SQL 的三驱动占位符
- `DeleteDeliveriesBefore` 改为窗口函数 + 批量删除（修复手动扫描字段
  与 SELECT 列表不一致的隐患）

## v0.5.1（性能/可扩展性专项）

- 数据保留策略落地：`traffic_samples` 按租户 `retention.rawDays` 定期清理
  （未配置时默认 30 天），housekeeping 每 10 分钟执行，防心跳高频写入
  导致数据库无界增长
- 操作历史按数量修剪：告警事件（20000/租户）、通知记录（10000/租户）、
  配置下发（200/节点）、配置修订（50/网络），与既有审计/命令修剪一致
- 心跳写放大优化：Agent 心跳改用 `UpdateNodeStatus` 只更新动态状态列，
  不再重写静态配置与加密私钥（每 10 秒/节点的整行重写消除）
- 补充 `traffic_samples(tenant_id, recorded_at)` 保留清理索引；
  新增 `ListTenants` 支撑按租户保留策略
- 新增 6 项专项测试（保留策略、租户枚举、心跳静态列保护）

## v0.5.0（S7-S14 中危项专项）

- S7 SSO：OIDC 外呼（discovery/JWKS/token/userinfo）统一走私网过滤拨号
  （防 SSRF，`WIREMESH_SSO_ALLOW_PRIVATE=1` 可放开本地测试）；redirect_uri
  由 Host 头构建时校验字符集，且登录/回调两端强制一致（防授权码劫持）；
  issuer 仅允许 http/https 完整 URL；前端 `location.href` 仅放行 http/https
- S8 Agent：`--mtls` 材料缺失时 fail-closed（拒绝启动），不再静默回退
  X-Agent-ID 头；探活（URL 发现）与主客户端分离
- S9 Agent 证书：新增 `POST /agent/v1/renew-cert` 续期端点（mTLS 认证，
  签发新证书并覆盖登记指纹）；`agentNode` 校验证书指纹与登记一致，
  轮换/吊销即时生效（等效 CRL）；Agent 到期前 30 天自动续期并重建传输
- S10 账户安全：修改密码增加按用户+IP 限流（5 次/15 分钟），启用 MFA 时
  必须提供动态验证码（otp_required/otp_invalid）；关闭 MFA 需当前密码 +
  动态验证码双重复核
- S11 内存上限：登录/改密/初始化限流表全局条目上限（10000），
  SSO state 表上限（5000），超限清理最旧条目，防内存放大
- S12 前端：任意已认证请求 401（会话过期/用户停用删除）时清空 app/mesh
  内存数据并跳转登录页，登录接口自身的 401 不触发
- S13 通知注入：渲染前按渠道净化节点名/消息等不信任字段——HTML/markdown
  类渠道（Telegram HTML、钉钉/企微/飞书）HTML 转义，全部渠道剥离控制字符
- S14 密码学参数：master key 经 Argon2id KDF 派生（旧 SHA-256 数据解密
  回退兼容）；bcrypt cost 10→12；TOTP 密钥 20→32 字节（SHA-1 保留，
  RFC 6238 默认且主流认证器仅支持）；SQLite 主库/WAL/SHM 权限收紧 0600

## v0.4.20（P1 专项，未发布 tag 前为当前 HEAD）

- MySQL DSN 启用 clientFoundRows，修复三驱动 RowsAffected 语义不一致
- 全部 JSON 端点增加 1 MiB 请求体大小限制（防内存 DoS）
- 登录限流（邮箱+IP，15 分钟窗口 5 次）与 `otp_invalid` 错误区分
- 内存凭据表定期清理（吊销令牌/会话/SSO state）
- 告警事件与通知记录改为分页接口（limit/offset + has_more）
- OIDC：校验 ID token 签名（JWKS）/issuer/audience/exp/nonce，检查 email_verified
- 登录与 SSO Cookie 增加 Secure 标志（HTTPS 下）
- 审计分页不被全局轮询重置；错误 toast 8 秒内去重
- 前端死代码清理（echarts 依赖移除，npm audit 0 漏洞）
- 仓库卫生：GeoLite2-City.mmdb 与 wiremesh-database.json 退出 git 跟踪
- 新增 GitHub Actions CI（vet/test/build/audit）

## v0.4.19（P0 安全加固）

- 强制 WIREMESH_MASTER_KEY，移除开发密钥回退
- Agent CA 持久化（wiremesh-ca.json），重启不再吊销证书
- 直连 TLS 强制 Agent 客户端证书（可配置代理回退）
- SSO 回调租户归属校验；修订私钥加密落库；会话超时接入配置
- 前端异步竞态修复（串行队列 + requestID）

## v0.4.18（系统设置功能专项）

- 用户管理：启停/角色/删除 + 最后管理员守卫 + 会话吊销
- 审计日志筛选、通知记录筛选、确认弹窗与统一组件

## v0.4.17（系统设置布局重构）

- 设置项四组分类导航、页头、未保存修改保护、tab 记忆

## v0.4.16（DNS 管理专项 15 项）

- 记录编辑、同名冲突友好提示、hosts 导出、客户端校验、自动映射去重

## v0.4.15（访问策略二轮专项 14 项）

- 级联清理源引用、拓扑配对提示、启停开关、发布确认等

## v0.4.14（访问策略专项 16 项）

- 资源编辑、引用删除保护、全部节点语义、未发布变更追踪

## v0.4.13（告警中心专项 14 项）

- 静默期持久化、恢复通知、规则作用域、立即评估、历史清空与筛选

## v0.4.12（客户端接入专项 15 项）

- 列表分组、拓扑提示、私钥提示、错误本地化、编辑入口

## v0.4.11（首页空状态调整）

## v0.4.10（节点列表专项 12 项）

- 批量操作、速率趋势、整行展开、汇总条、断点下移

## v0.4.9（首页信息架构专项 10 项）

- 移除假数据、实时速率卡、更新指示、告警摘要、地图交互

## v0.4.8（地图默认中国视角）

## v0.4.7（新增 13 项功能）

- 客户端接入、告警中心、访问策略、DNS 管理、MFA、SSO、API 令牌、备份等

## v0.4.6（前后端审查修复与安全加固）

## v0.4.5（存储层加固、SSRF 修复与热路径优化）

## 更早版本

- v0.4.4 节点列表累计流量统计；v0.4.3 地图默认中国；v0.4.2 布局优化；
  v0.4.1 逻辑优化；v0.4.0 wireproto 与 SQL schema builder 优化；
  v0.3.x 与 v0.2.x 为早期开发线。

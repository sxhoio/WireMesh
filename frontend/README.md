# WireMesh 前端控制台

Vue 3 + TypeScript + Vite 实现的 WireMesh 多租户 WireGuard 管理控制台。

- 开发：`npm run dev`（默认 http://localhost:5173）
- 类型检查与构建：`npm run build`（vue-tsc -b && vite build，产物在 `dist/`）
- 依赖安装：`npm ci`

生产构建由后端 `cmd/wiremesh-server` 通过 `WIREMESH_WEB_DIR=frontend/dist` 静态托管；
路由采用 hash 模式，构建产物无需服务端重写规则。

## 目录结构

- `src/api.ts` — 后端 API 契约与统一请求通道（含鉴权头）
- `src/stores/` — Pinia 状态（app 会话 / mesh 拓扑数据）
- `src/views/` — 页面（首页 / 节点 / 客户端接入 / 告警 / 访问策略 / DNS / 系统设置）
- `src/components/` — 地图、弹窗、图表等组件
- `src/types/` — 领域模型（与 `api.ts` 的 Api* 传输类型互补）

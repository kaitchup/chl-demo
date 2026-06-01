# AGENTS.md — chl-demo 项目索引

> 给 AI code agent（Codex / Claude Code / Reasonix 等）读的入口文件。
> 打开本目录请先读这里，再按需进对应文档/代码。

## 这是什么
语言学习平台的 **USDT 会员订阅系统**。用户用 USDT 订阅 Super 会员，解锁全部语种学习资源。
支付走第三方加密货币收单网关 **UPay**（按法币计价、USDT 链上结算）。

- 技术栈：后端 **Go + Echo + pgx**（手写 repo + 内嵌迁移器，非 sqlc）；前端 **React + Vite + TanStack Query + Tailwind**；数据库 **PostgreSQL**；反代 **nginx**；部署 **Docker Compose**。
- 当前版本：**v0.0.2** — UPay 真实支付代码已就绪（创建订单/收银台/回调验签/轮询兜底/开通会员），**双模式**：未配 `UPAY_API_KEY` 自动回退占位收银台（本地无 NetBird 可跑）。待真实环境联调 + 核对验签口径。

## 文档地图（先看这些）
| 文档 | 内容 |
|---|---|
| [README.md](README.md) | 快速上手、本地开发、部署步骤 |
| [docs/PRD-会员订阅与USDT支付.md](docs/PRD-会员订阅与USDT支付.md) | 需求文档（v0.2，已按真实 UPay 接口核对） |
| [docs/TECH-DESIGN-技术方案.md](docs/TECH-DESIGN-技术方案.md) | 技术方案（数据模型、API、UPay 集成、回调验签、轮询兜底） |
| [docs/TODO.md](docs/TODO.md) | 待办 / Roadmap（v0.0.2 真实支付、VDS 部署、域名+HTTPS、Cloudflare） |
| [docs/TEST-ACCOUNTS.md](docs/TEST-ACCOUNTS.md) | 本地测试账号（含明文口令，**已 gitignore**） |

## 目录结构
```
chl-demo/
├── AGENTS.md / CLAUDE.md / README.md   # 索引 & 说明
├── docs/        # PRD / TECH-DESIGN / TEST-ACCOUNTS
├── certs/       # upay-local-ca.crt（UPay 自签 CA，NetBird 环境，v0.0.2 用）
├── backend/     # Go：cmd/server 入口；internal/{handler,repo,auth,middleware}；db/migrations
├── frontend/    # React：src/{api,auth,components,pages}
└── deploy/      # docker-compose.yml + nginx/chl.conf（VDS 监听 80）
```

## 本地怎么跑（已验证）
后端在 **:8090**（`PORT` 可配；本地用 8090 因宿主 nginx 默认占了 8080）：
```bash
cd backend
DATABASE_URL="postgres://postgres:postgres@localhost:5432/chl?sslmode=disable" \
JWT_SECRET=dev-secret PUBLIC_BASE_URL=http://localhost:8088 PORT=8090 go run ./cmd/server
```
前端经宿主 nginx（`/opt/homebrew/etc/nginx/servers/chl.conf`）在 **:8088** 提供静态 `frontend/dist`，`/api/`→8090。
访问 **http://localhost:8088**。改前端需 `cd frontend && npm run build`（或 `npm run dev` 走 :5173 热更新）。

## 关键约定（改代码前必读）
- **API 统一返回**：`{ "data": ..., "error": null }`；错误 `{ "data": null, "error": { "code", "message" } }`。
- **鉴权**：`Authorization: Bearer <JWT>`（HS256）。受限资源走会员校验中间件。
- **金额**：全程 decimal/字符串，**禁用 float**；下单金额后端按 `plan_code` 算，不信任前端。
- **密钥**：只从环境变量读，不入库、不入仓、不进前端。
- **DB 迁移**：后端启动时自动执行 `backend/db/migrations/*.up.sql`（内嵌极简迁移器，幂等）。

## 待办 / Roadmap
完整清单（v0.0.2 真实支付 + VDS 部署 + 域名 HTTPS + Cloudflare）见 **[docs/TODO.md](docs/TODO.md)**。下面是最近的开发项概要：

### v0.0.2（接 UPay 真实支付）
创建订单调 UPay → 内嵌/跳转收银台 → 回调 **HMAC-SHA256 验签**（原始字节、5 分钟防重放、双签兼容）→ 轮询兜底（超时/失败 UPay 不发回调）→ 开通会员（事务+行锁+幂等）。细节见 [docs/TECH-DESIGN-技术方案.md](docs/TECH-DESIGN-技术方案.md) §6/§9。
> UPay 测试环境仅经 NetBird VPN 可达，CA 在 `certs/upay-local-ca.crt`。

# 语言学习平台 · 会员订阅（chl_demo）

USDT 会员订阅系统。后端 Go + Echo + pgx，前端 React + Vite + TanStack Query，数据库 PostgreSQL，Caddy 反代，Docker Compose 部署。

- 需求文档：[docs/PRD-会员订阅与USDT支付.md](docs/PRD-会员订阅与USDT支付.md)
- 技术方案：[docs/TECH-DESIGN-技术方案.md](docs/TECH-DESIGN-技术方案.md)

## 版本

### v0.0.1 — 前后端 + 数据库全链路打通
账号体系、套餐读库、下单写库（占位收银台）、订单列表、启动自动迁移。

### v0.0.2（当前）— UPay 真实支付（代码已就绪，待联调）
- 创建订单调 UPay `POST /v1/payment/request`（Idempotency-Key、metadata 透传 user_id/plan_code）
- 收银台页内嵌 `checkout_url`（iframe + 新标签兜底）+ 轮询订单状态到终态
- 回调 `POST /api/webhooks/upay`：**HMAC-SHA256 验签**（原始字节、5 分钟防重放、双签轮换、event.id 幂等）— 已含单元测试
- 兜底轮询任务发现超时/失败（UPay 不发回调）并补开通漏单
- 开通会员：事务 + 行锁 + 幂等 + 按周期叠加到期时间
- **双模式**：未配置 `UPAY_API_KEY` 时自动回退占位收银台 → 本地无 NetBird 也能跑
- ⏳ 待办：在能访问 UPay（NetBird）的环境用真实凭据 + 一条真实回调样本联调，核对 `signing_secret` 验签口径（见技术方案 §6.2 注）

---

## 目录
```
backend/    Go 服务（cmd/server 入口；internal/{handler,service,repo,upay,job,middleware}；db/migrations）
frontend/   React + Vite（src/{api,auth,components,pages}）
deploy/     docker-compose.yml + nginx/chl.conf + .env.example
docs/       PRD / TECH-DESIGN / TODO / TEST-ACCOUNTS
certs/      upay-local-ca.crt
```

## 本地开发（不用 Docker）

需要本地 PostgreSQL。先建库：
```bash
createdb chl   # 或 psql -c "CREATE DATABASE chl;"
```

后端：
```bash
cd backend
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/chl?sslmode=disable"
export JWT_SECRET="dev-secret"
export PUBLIC_BASE_URL="http://localhost:5173"
go run ./cmd/server          # 启动时自动迁移 + 灌入套餐，监听 :8080
```

前端：
```bash
cd frontend
npm install
npm run dev                  # http://localhost:5173 （/api 自动代理到 :8080）
```

打开 http://localhost:5173 → 注册 → 订阅页 → 下单 → 订单列表。

## 部署到 VDS（Docker Compose + 宿主机 nginx）

边缘反代用你 VDS 上的 **nginx**（不用 Caddy）。docker 只跑 db / backend / frontend，
分别监听 `127.0.0.1:8080` 和 `127.0.0.1:8081`，由 nginx 统一对外。

**1) 起容器**
```bash
cd deploy
cp .env.example .env          # 改 JWT_SECRET / DB_PASS / PUBLIC_BASE_URL
docker compose up -d --build
# 验证：curl http://127.0.0.1:8080/api/healthz  和  curl http://127.0.0.1:8081/
```

**2) 配 nginx 反代**
```bash
sudo cp nginx/chl.conf /etc/nginx/sites-available/chl.conf
sudo ln -s /etc/nginx/sites-available/chl.conf /etc/nginx/sites-enabled/
# 把 chl.conf 里的 server_name 改成你的域名（或保留 _ 用 IP 访问）
sudo nginx -t && sudo systemctl reload nginx
```
反代规则（见 [deploy/nginx/chl.conf](deploy/nginx/chl.conf)）：`/api/` → `127.0.0.1:8080`（后端），`/` → `127.0.0.1:8081`（前端）。前后端同源，无需 CORS。

**3) HTTPS（可选）**
```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d pay.你的域名.com   # 自动加证书 + 80→443 跳转
```

**用已有本机 PostgreSQL**：注释掉 compose 里的 `db` 服务，在 `.env` 设
`DATABASE_URL=postgres://postgres:postgres@host.docker.internal:5432/chl?sslmode=disable`。

## 工程取舍说明
- v0.0.1 用手写 pgx + 内嵌极简迁移器（启动即迁移），不依赖 sqlc / golang-migrate 二进制，保证开箱即建。目录结构保持可平滑切换到 sqlc（见技术方案）。
- 所有密钥经环境变量注入，不入库、不入仓、不进前端。

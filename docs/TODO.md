# TODO / Roadmap

本项目待办清单（活文档，完成后打勾并补充实际操作记录）。当前版本 **v0.0.1**（本地全链路打通）。

## 开发

- [~] **v0.0.2 接入 UPay 真实支付**（代码已就绪，待真实环境联调）
  - [x] 创建订单调 UPay `POST /v1/payment/request`（`Idempotency-Key`、`metadata` 存 user_id/plan_code）
  - [x] 收银台页跳转 `checkout_url` + 轮询订单状态到终态
  - [x] **收银台改为整页跳转，不再用 iframe 内嵌**：UPay 托管收银台 SPA（`checkout.upay.local`）的会话 Cookie `cks_cs_*` 是 `SameSite=Lax/Strict`，iframe 内嵌属跨站第三方上下文会被浏览器拒收 → 页面空白。改为点击按钮整页跳到 `checkout_url`，付款后经 `success_url` 跳回 `/checkout/:id/result`，由轮询接管开通（同 Stripe Checkout 的做法）。改在 `frontend/src/pages/Checkout.tsx`
  - [x] 回调 `POST /api/webhooks/upay`：**HMAC-SHA256 验签**（原始字节、5 分钟防重放、双签兼容、event.id 幂等）— 含单元测试
  - [x] 轮询兜底定时任务（超时 EXPIRED / 失败 FAILED 不发回调，只能轮询发现）
  - [x] 回调/轮询命中后开通会员（事务 + 行锁 + `applied_to_subscription` 幂等）
  - [x] 双模式：未配 `UPAY_API_KEY` 回退占位收银台（本地无 NetBird 可跑）
  - [ ] **联调**：在能访问 UPay（NetBird）的环境用真实 `sk_test_` 凭据跑通下单→收银台→回调→开通
  - [ ] **核对 `signing_secret` 验签口径**（整串 `whsec_` vs 剥前缀/解码）——用一条真实回调样本固化（TECH §6.2 注）
  - 依赖：UPay 测试环境仅经 NetBird VPN 可达；CA 在 [`certs/upay-local-ca.crt`](../certs/upay-local-ca.crt)
  - 设计细节见 [TECH-DESIGN-技术方案.md](TECH-DESIGN-技术方案.md) §6 / §9

## 部署 / 运维

- [ ] **1. 用 nginx + docker-compose 部署到 hostVDS**
  - VDS：`185.92.181.220`，建议 Ubuntu 22.04/24.04 LTS，装 Docker + compose 插件
  - 起容器：`cd deploy && cp .env.example .env`（改 `JWT_SECRET`/`DB_PASS`/`PUBLIC_BASE_URL`）→ `docker compose up -d --build`
  - 容器只绑 `127.0.0.1`（backend:8080 / frontend:8081），由宿主机 nginx 反代
  - nginx：`deploy/nginx/chl.conf` → `/etc/nginx/sites-available/`，`/api/`→8080、`/`→8081
  - 自检：`curl http://127.0.0.1:8080/api/healthz`

- [ ] **2. 购买域名 + HTTPS 证书，指向 hostVDS `185.92.181.220`**
  - 买域名（如 `pay.example.com`），加 **A 记录 → 185.92.181.220**
  - 证书二选一（与第 3 项 Cloudflare 方案相关，见下方「注意」）：
    - 不挂 CF：`sudo certbot --nginx -d pay.example.com`（Let's Encrypt 自动签 + 80→443 跳转）
    - 挂 CF：用 **Cloudflare Origin Certificate** 装到 VDS nginx（15 年有效，仅 CF↔源站用）
  - 改 `deploy/nginx/chl.conf` 的 `server_name` 为真实域名

- [ ] **3. 用 Cloudflare 做防护 + CDN 加速**
  - 域名 NS 接入 Cloudflare；DNS 记录开**橙色云**（代理）
  - SSL/TLS 模式设 **Full (strict)**，源站用 CF Origin Certificate（配合第 2 项）
  - 防护：开 WAF、Rate Limiting、Bot Fight；**不要缓存 `/api/*`**（Cache Rule 跳过），静态资源走缓存
  - 真实客户端 IP：CF 经 `CF-Connecting-IP` 传入，nginx 用 `set_real_ip_from` + `real_ip_header CF-Connecting-IP` 还原（影响后端限流/日志）

> **注意（2↔3 的关系）**：一旦走 Cloudflare 代理，边缘 TLS 由 CF 终止，源站证书改用 **CF Origin Certificate**，第 2 项的 certbot/Let's Encrypt 就不再必需。建议直接按「CF 代理 + Origin Cert + Full(strict)」一条路线落地，避免重复签证书。

# 语言学习平台 · 会员订阅与 USDT 支付 技术方案（TDD）

| 项 | 内容 |
|---|---|
| 文档版本 | v0.1 |
| 日期 | 2026-06-01 |
| 关联 | [PRD-会员订阅与USDT支付.md](./PRD-会员订阅与USDT支付.md)（v0.2） |
| 技术栈 | Go + Echo + sqlc + golang-migrate / React + Vite + TanStack Query + shadcn/ui / PostgreSQL / Caddy / Docker Compose |

本方案给出工程结构、数据库迁移、后端核心实现骨架（**重点是 UPay 回调验签、会员开通事务、兜底轮询三处高风险逻辑**）、平台 API 详细契约、前端关键实现与部署配置。代码为可落地骨架，省略非核心样板。

---

## 1. 设计原则

1. **支付结果以 UPay 服务端为准**：回调体不直接用于发货，验签后以 `data.id` 反查 `GET /v1/payment/request/{id}` 拿权威状态再开通。
2. **三重幂等**：创建订单（`Idempotency-Key`）、回调处理（`event.id` 去重）、会员开通（`orders.applied_to_subscription` + 行锁）。
3. **轮询是必需路径**，不是可选优化：超时(EXPIRED)/失败(FAILED) UPay 不发回调，只能靠轮询发现（见 PRD §9.4）。
4. **金额全程 decimal/字符串**，禁用 float；后端按 `plan_code` 计算金额，不信任前端。
5. **密钥只从环境变量读**，不入库、不入仓、不进前端。

---

## 2. 工程结构

```
chl_demo/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── config/            # 环境变量加载
│   │   ├── handler/           # auth, order, subscription, webhook
│   │   ├── service/           # account, membership, payment
│   │   ├── repo/              # sqlc 生成 (sqlc.gen.go) + queries
│   │   ├── upay/              # client.go, signature.go, types.go
│   │   ├── middleware/        # jwt.go, membership.go
│   │   └── job/               # reconcile.go (兜底轮询)
│   ├── db/
│   │   ├── migrations/        # golang-migrate *.up.sql / *.down.sql
│   │   └── queries/           # sqlc *.sql
│   ├── sqlc.yaml
│   ├── go.mod
│   └── Dockerfile
├── frontend/
│   ├── src/
│   │   ├── api/               # client.ts, orders.ts, auth.ts
│   │   ├── hooks/             # useOrderStatus, useAuth, usePlans
│   │   ├── pages/             # Login, Register, Membership, Checkout, Account...
│   │   ├── components/        # ui/(shadcn), PlanCard, RequireAuth, RequireMember
│   │   └── lib/
│   ├── vite.config.ts
│   └── Dockerfile
├── deploy/
│   ├── docker-compose.yml
│   └── nginx/chl.conf
├── docs/                      # PRD / TECH-DESIGN / TEST-ACCOUNTS
└── certs/
    └── upay-local-ca.crt      # UPay 自签 CA（NetBird 测试环境）
```

---

## 3. 配置与环境变量

| 变量 | 说明 |
|---|---|
| `DATABASE_URL` | `postgres://user:pass@db:5432/chl?sslmode=disable` |
| `JWT_SECRET` | JWT 签名密钥（HS256） |
| `JWT_TTL` | access token 有效期，默认 `24h` |
| `UPAY_BASE_URL` | `https://api.upay.local`（测试） |
| `UPAY_API_KEY` | `sk_test_<key_id>.<secret>`（scope: payments:read+write） |
| `UPAY_WEBHOOK_SECRET` | 回调验签 `whsec_...`（支持逗号分隔双密钥用于轮换期） |
| `UPAY_CA_CERT` | `/app/upay-local-ca.crt`（自签 CA 路径，注入 HTTP client） |
| `PUBLIC_BASE_URL` | 平台对外域名，用于拼 `success_url`/`cancel_url` |
| `RECONCILE_INTERVAL` | 兜底轮询周期，默认 `60s` |

> 测试环境 UPay 仅经 NetBird 可达；容器需加入 NetBird 或宿主机共享隧道，否则连接超时。

---

## 4. 数据库迁移（golang-migrate）

`db/migrations/0001_init.up.sql`：

```sql
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE membership_plans (
    code          TEXT PRIMARY KEY,            -- monthly | quarterly | yearly
    name          TEXT NOT NULL,
    duration_days INT  NOT NULL,
    amount        NUMERIC(18,2) NOT NULL,      -- 计价金额
    currency      TEXT NOT NULL DEFAULT 'USD',
    active        BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE subscriptions (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL UNIQUE REFERENCES users(id),
    tier       TEXT NOT NULL DEFAULT 'super',
    expires_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    id                      BIGSERIAL PRIMARY KEY,
    merchant_order_id       TEXT NOT NULL UNIQUE,     -- sub_<ULID>
    upay_payment_id         TEXT UNIQUE,              -- pay_<ULID>
    user_id                 BIGINT NOT NULL REFERENCES users(id),
    plan_code               TEXT NOT NULL REFERENCES membership_plans(code),
    amount                  NUMERIC(18,2) NOT NULL,
    currency                TEXT NOT NULL DEFAULT 'USD',
    status                  TEXT NOT NULL DEFAULT 'PENDING',  -- PENDING/PAID/EXPIRED/CANCELED/FAILED
    received_amount         NUMERIC(18,2) NOT NULL DEFAULT 0,
    failure_code            TEXT,
    checkout_url            TEXT,
    idempotency_key         TEXT,
    applied_to_subscription BOOLEAN NOT NULL DEFAULT false,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at                 TIMESTAMPTZ,
    expires_at              TIMESTAMPTZ
);
CREATE INDEX idx_orders_user      ON orders(user_id, created_at DESC);
CREATE INDEX idx_orders_status    ON orders(status) WHERE status = 'PENDING';

CREATE TABLE webhook_events (
    id              BIGSERIAL PRIMARY KEY,
    upay_event_id   TEXT NOT NULL UNIQUE,            -- evt_<ULID> 幂等去重键
    upay_payment_id TEXT,
    event_type      TEXT,
    payload         JSONB,
    signature_valid BOOLEAN,
    processed       BOOLEAN NOT NULL DEFAULT false,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO membership_plans(code,name,duration_days,amount,currency) VALUES
  ('monthly',  'Super 月付', 30,  20.00, 'USD'),
  ('quarterly','Super 季付', 90,  54.00, 'USD'),
  ('yearly',   'Super 年付', 365, 192.00,'USD');
```

> `0001_init.down.sql` 按依赖反序 `DROP TABLE`（webhook_events → orders → subscriptions → membership_plans → users）。

---

## 5. sqlc 查询示例

`db/queries/orders.sql`：

```sql
-- name: CreateOrder :one
INSERT INTO orders (merchant_order_id, user_id, plan_code, amount, currency, idempotency_key)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: AttachUpayResult :exec
UPDATE orders SET upay_payment_id=$2, checkout_url=$3, expires_at=$4, status='PENDING'
WHERE id=$1;

-- name: GetOrderForUpdate :one
SELECT * FROM orders WHERE upay_payment_id=$1 FOR UPDATE;

-- name: ListPendingOrders :many
SELECT * FROM orders WHERE status='PENDING' ORDER BY created_at;

-- name: ReuseOpenOrder :one
SELECT * FROM orders
WHERE user_id=$1 AND status='PENDING' AND expires_at > now()
ORDER BY created_at DESC LIMIT 1;
```

`sqlc.yaml` 用 `pgx/v5` 引擎，生成类型安全 `repo.Queries`。

---

## 6. 后端核心实现

### 6.1 UPay 客户端 `internal/upay/client.go`

```go
package upay

import (
    "bytes"
    "crypto/tls"
    "crypto/x509"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "time"
)

type Client struct {
    baseURL string
    apiKey  string
    http    *http.Client
}

func New(baseURL, apiKey, caPath string) (*Client, error) {
    pool := x509.NewCertPool()
    if caPath != "" {
        pem, err := os.ReadFile(caPath)
        if err != nil { return nil, err }
        pool.AppendCertsFromPEM(pem)
    }
    return &Client{
        baseURL: baseURL, apiKey: apiKey,
        http: &http.Client{
            Timeout: 15 * time.Second,
            Transport: &http.Transport{TLSClientConfig: &tls.Config{
                RootCAs: pool, MinVersion: tls.VersionTLS12,
            }},
        },
    }, nil
}

type CreateOrderReq struct {
    MerchantOrderID  string            `json:"merchant_order_id"`
    Amount           string            `json:"amount"`            // "20.00"
    Currency         string            `json:"currency"`          // "USD"
    Description      string            `json:"description,omitempty"`
    ExpiresInSeconds int               `json:"expires_in_seconds,omitempty"` // 1800
    SuccessURL       string            `json:"success_url,omitempty"`
    CancelURL        string            `json:"cancel_url,omitempty"`
    Metadata         map[string]string `json:"metadata,omitempty"` // user_id, plan_code
}

type Order struct {
    ID              string `json:"id"`
    Status          string `json:"status"`
    Amount          string `json:"amount"`
    Currency        string `json:"currency"`
    MerchantOrderID string `json:"merchant_order_id"`
    ReceivedAmount  string `json:"received_amount"`
    FailureCode     string `json:"failure_code"`
    CheckoutURL     string `json:"checkout_url"`
    PaidAt          string `json:"paid_at"`
    ExpiresAt       string `json:"expires_at"`
    CreatedAt       string `json:"created_at"`
}

func (c *Client) CreateOrder(r CreateOrderReq, idempotencyKey string) (*Order, error) {
    body, _ := json.Marshal(r)
    req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/v1/payment/request", bytes.NewReader(body))
    req.Header.Set("Authorization", "Bearer "+c.apiKey)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Idempotency-Key", idempotencyKey) // 必填，缺失→400
    return c.do(req)
}

func (c *Client) GetOrder(payID string) (*Order, error) {
    req, _ := http.NewRequest(http.MethodGet, c.baseURL+"/v1/payment/request/"+payID, nil)
    req.Header.Set("Authorization", "Bearer "+c.apiKey)
    return c.do(req)
}

func (c *Client) do(req *http.Request) (*Order, error) {
    resp, err := c.http.Do(req)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    raw, _ := io.ReadAll(resp.Body)
    if resp.StatusCode/100 != 2 {
        return nil, fmt.Errorf("upay %d: %s", resp.StatusCode, raw)
    }
    var o Order
    if err := json.Unmarshal(raw, &o); err != nil { return nil, err }
    return &o, nil
}
```

### 6.2 回调验签 `internal/upay/signature.go`（⭐ 最高风险，逐条对齐文档）

```go
package upay

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "strconv"
    "strings"
    "time"
)

const tolerance = 5 * time.Minute // 防重放：t 与本机时间差 > 5 分钟拒绝

// VerifyWebhook 校验 UPay-Signature 头。
// rawBody 必须是收到的原始字节（勿反序列化后重编码）。
// secrets 支持多个（双签轮换期：新、旧密钥），任一验过即接受。
func VerifyWebhook(sigHeader string, rawBody []byte, secrets []string, now time.Time) error {
    ts, vs, err := parseSigHeader(sigHeader) // 解析 t= 与所有 v=
    if err != nil { return err }

    // 1. 防重放
    if d := now.Sub(time.Unix(ts, 0)); d > tolerance || d < -tolerance {
        return errors.New("signature timestamp out of tolerance")
    }

    // 2. 待签数据 = t的十进制文本 + "." + 原始body字节
    signed := append([]byte(strconv.FormatInt(ts, 10)+"."), rawBody...)

    // 3. 对每个密钥重算，与头里每个 v= 常量时间比较
    for _, secret := range secrets {
        mac := hmac.New(sha256.New, []byte(secret)) // 见下方“密钥口径”注
        mac.Write(signed)
        want := []byte(hex.EncodeToString(mac.Sum(nil))) // 小写 hex
        for _, v := range vs {
            if hmac.Equal(want, []byte(v)) {
                return nil
            }
        }
    }
    return errors.New("no matching signature")
}

func parseSigHeader(h string) (ts int64, vs []string, err error) {
    for _, part := range strings.Split(h, ",") {
        kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
        if len(kv) != 2 { continue }
        switch kv[0] {
        case "t":
            ts, err = strconv.ParseInt(kv[1], 10, 64)
            if err != nil { return 0, nil, err }
        case "v":
            vs = append(vs, kv[1]) // 双签期会有多个 v=
        }
    }
    if ts == 0 || len(vs) == 0 { return 0, nil, errors.New("malformed UPay-Signature") }
    return ts, vs, nil
}
```

> **密钥口径待用样本核对**：文档写「用 `signing_secret`（`whsec_` 前缀 + 43 字符）做 HMAC」，未明确是否需剥前缀或 base64 解码。骨架按「整串 `whsec_...` 作为 HMAC key」实现；联调时用一条真实回调比对 `v`，若不符再尝试「剥 `whsec_` 后取 43 字符」或「解码后字节」。这是上线前必须用真实样本验证的一处。

### 6.3 回调 handler `internal/handler/webhook.go`

```go
func (h *Handler) UpayWebhook(c echo.Context) error {
    raw, _ := io.ReadAll(c.Request().Body) // 必须先取原始字节再验签

    if err := upay.VerifyWebhook(
        c.Request().Header.Get("UPay-Signature"), raw, h.cfg.WebhookSecrets, time.Now(),
    ); err != nil {
        return c.NoContent(http.StatusBadRequest) // 验签失败丢弃
    }

    var evt upay.Event
    if err := json.Unmarshal(raw, &evt); err != nil {
        return c.NoContent(http.StatusBadRequest)
    }

    // event.id 幂等去重：插入 webhook_events，冲突即已处理过 → 直接 2xx
    if dup, _ := h.repo.InsertWebhookEvent(c.Request().Context(), evt, raw); dup {
        return c.NoContent(http.StatusOK)
    }

    // 仅 paid 触发开通；以 data.id 反查权威状态后开通（见 6.4）
    if evt.Event == "payment_order.paid" {
        if err := h.payment.ConfirmAndActivate(c.Request().Context(), evt.Data.ID); err != nil {
            // 处理失败：不返回 2xx，让 UPay 退避重试；轮询也会兜底
            return c.NoContent(http.StatusInternalServerError)
        }
    }
    // partially_paid / 其它：记录即可，保持订单 PENDING
    return c.NoContent(http.StatusOK) // 尽快 2xx，平台只看状态码
}
```

### 6.4 会员开通（事务 + 行锁 + 幂等）`internal/service/payment.go`

```go
// ConfirmAndActivate：以 UPay 权威状态为准开通会员；回调与轮询共用，可重复安全调用。
func (s *PaymentService) ConfirmAndActivate(ctx context.Context, payID string) error {
    o, err := s.upay.GetOrder(payID) // 反查权威状态
    if err != nil { return err }
    if o.Status != "PAID" { // 未足额则不开通（partially_paid 等）
        return s.syncStatus(ctx, payID, o)
    }

    return s.tx(ctx, func(q *repo.Queries) error {
        ord, err := q.GetOrderForUpdate(ctx, payID) // SELECT ... FOR UPDATE
        if err != nil { return err }
        if ord.AppliedToSubscription {              // 已开通 → 幂等返回
            return nil
        }
        plan, err := q.GetPlan(ctx, ord.PlanCode)
        if err != nil { return err }

        // 叠加规则：new = max(now, current_expires) + duration
        sub, _ := q.GetSubscriptionForUpdate(ctx, ord.UserID) // 行锁防并发叠加
        base := time.Now()
        if sub.ExpiresAt.Valid && sub.ExpiresAt.Time.After(base) {
            base = sub.ExpiresAt.Time
        }
        newExpiry := base.AddDate(0, 0, int(plan.DurationDays))

        if err := q.UpsertSubscription(ctx, ord.UserID, "super", newExpiry); err != nil {
            return err
        }
        return q.MarkOrderPaid(ctx, ord.ID, parseTime(o.PaidAt)) // status=PAID, applied=true, paid_at
    })
}
```

要点：`GetOrderForUpdate` + `GetSubscriptionForUpdate` 两把行锁串行化同用户的并发开通；`applied_to_subscription` 保证单笔订单只叠加一次。

### 6.5 下单 handler `internal/handler/order.go`

```go
func (h *Handler) CreateOrder(c echo.Context) error {
    userID := middleware.UserID(c)
    var in struct{ PlanCode string `json:"plan_code"` }
    if err := c.Bind(&in); err != nil { return badRequest(c, "invalid_request") }

    plan, err := h.repo.GetPlan(c.Request().Context(), in.PlanCode)
    if err != nil { return badRequest(c, "plan_not_found") }

    // 复用最近未过期挂单
    if ord, err := h.repo.ReuseOpenOrder(c.Request().Context(), userID); err == nil {
        return ok(c, orderView(ord))
    }

    moid := "sub_" + ulid.Make().String()
    idemKey := moid + ":create"
    ord, _ := h.repo.CreateOrder(c.Request().Context(), repo.CreateOrderParams{
        MerchantOrderID: moid, UserID: userID, PlanCode: plan.Code,
        Amount: plan.Amount, Currency: plan.Currency, IdempotencyKey: idemKey,
    })

    up, err := h.upay.CreateOrder(upay.CreateOrderReq{
        MerchantOrderID:  moid,
        Amount:           plan.Amount.StringFixed(2), // "20.00"
        Currency:         plan.Currency,
        Description:      plan.Name,
        ExpiresInSeconds: 1800,
        SuccessURL:       h.cfg.PublicBaseURL + "/checkout/" + moid + "/result",
        CancelURL:        h.cfg.PublicBaseURL + "/checkout/" + moid + "/result",
        Metadata:         map[string]string{"user_id": fmt.Sprint(userID), "plan_code": plan.Code},
    }, idemKey)
    if err != nil { return upstreamError(c, err) }

    h.repo.AttachUpayResult(c.Request().Context(), ord.ID, up.ID, up.CheckoutURL, parseTime(up.ExpiresAt))
    return ok(c, gin.H{"order_id": moid, "checkout_url": up.CheckoutURL, "expires_at": up.ExpiresAt})
}
```

### 6.6 兜底轮询任务 `internal/job/reconcile.go`

```go
// 每 RECONCILE_INTERVAL 扫描 PENDING 订单，对齐 UPay 状态。
// 负责发现 EXPIRED/FAILED（无回调），并补开通漏掉的 PAID。
func (j *Reconciler) Run(ctx context.Context) {
    t := time.NewTicker(j.interval)
    for {
        select {
        case <-ctx.Done(): return
        case <-t.C:
            pend, _ := j.repo.ListPendingOrders(ctx)
            for _, o := range pend {
                if o.UpayPaymentID == "" { continue }
                up, err := j.upay.GetOrder(o.UpayPaymentID)
                if err != nil { continue }
                switch up.Status {
                case "PAID":
                    _ = j.payment.ConfirmAndActivate(ctx, o.UpayPaymentID) // 幂等
                case "EXPIRED", "FAILED":
                    _ = j.repo.MarkOrderTerminal(ctx, o.ID, up.Status, up.FailureCode)
                default:
                    _ = j.repo.UpdateReceived(ctx, o.ID, up.ReceivedAmount) // 更新部分到账
                }
            }
        }
    }
}
```

### 6.7 中间件

- `middleware/jwt.go`：解析 `Authorization: Bearer`，校验 HS256，注入 `user_id`；失败 401。
- `middleware/membership.go`：读 `subscriptions.expires_at`，`<= now` → 403 `membership_required`。仅包裹受限学习资源路由。

---

## 7. 平台 API 详细契约

> 统一响应 `{ "data": ..., "error": null }`。以下省略 `data` 外层。

| 端点 | 请求 | 响应（data） |
|---|---|---|
| `POST /api/auth/register` | `{email, password}` | `{user_id, token}` |
| `POST /api/auth/login` | `{email, password}` | `{token, expires_at}` |
| `GET /api/me` | — | `{email, member: {tier, status, expires_at}}` |
| `GET /api/plans` | — | `[{code, name, amount, currency, duration_days}]` |
| `GET /api/subscription` | — | `{tier, status: active|expired|none, expires_at, days_left}` |
| `POST /api/orders` | `{plan_code}` | `{order_id, checkout_url, expires_at}` |
| `GET /api/orders?status=&page=` | — | `{items:[...], total, page}` |
| `GET /api/orders/:id` | — | 订单全字段（含 received_amount, failure_code, paid_at） |
| `GET /api/orders/:id/status` | — | `{status, received_amount, expires_at}`（前端轮询） |
| `POST /api/orders/:id/cancel` | — | `{status: CANCELED}` |
| `POST /api/webhooks/upay` | UPay 事件 | 2xx（无 body） |

错误码示例：`401 unauthorized` / `403 membership_required` / `400 plan_not_found` / `409 order_exists` / `502 upstream_error`（UPay 不可用）。

---

## 8. 前端关键实现

### 8.1 API client `src/api/client.ts`
- Axios 实例，注入 `Authorization`；401 拦截跳 `/login`。
- 统一解包 `{data,error}`，error 抛出供 TanStack Query 捕获。

### 8.2 路由守卫
```tsx
<RequireAuth>           {/* 无 token → /login */}
  <RequireMember>       {/* 非会员访问学习资源 → /membership */}
    <LearningResource/>
  </RequireMember>
</RequireAuth>
```

### 8.3 收银台页轮询 `src/pages/Checkout.tsx`
```tsx
const { data } = useQuery({
  queryKey: ["order-status", orderId],
  queryFn: () => api.get(`/orders/${orderId}/status`),
  refetchInterval: (q) =>
    ["PAID","EXPIRED","FAILED","CANCELED"].includes(q.state.data?.status) ? false : 4000,
});
// PAID → 跳 /account/subscription；EXPIRED/FAILED → 结果页提示重下单
// 收银台呈现：先尝试 <iframe src={checkout_url}>，onError/被 CSP 拦截则整页 window.location = checkout_url
```
- 同时显示到 `expires_at` 的倒计时；过期禁用 iframe，提示重新下单。

### 8.4 订阅页 / 个人中心 / 订单列表/详情
- `PlanCard` 三张周期卡片，标注 `≈ {amount} USDT`，点击 `POST /api/orders` 后跳 `/checkout/:orderId`。
- 订单列表用 `GET /api/orders` 分页 + 状态 Tab 筛选；详情展示全字段，待支付给「继续支付/取消」。

---

## 9. 部署

`deploy/docker-compose.yml`（要点）：
```yaml
services:
  db:
    image: postgres:17-alpine
    environment: { POSTGRES_DB: chl, POSTGRES_USER: chl, POSTGRES_PASSWORD: ${DB_PASS} }
    volumes: [ "pgdata:/var/lib/postgresql/data" ]
  backend:
    build: ../backend
    environment:
      DATABASE_URL: postgres://chl:${DB_PASS}@db:5432/chl?sslmode=disable
      UPAY_BASE_URL: https://api.upay.local
      UPAY_API_KEY: ${UPAY_API_KEY}
      UPAY_WEBHOOK_SECRET: ${UPAY_WEBHOOK_SECRET}
      UPAY_CA_CERT: /app/upay-local-ca.crt
      JWT_SECRET: ${JWT_SECRET}
      PUBLIC_BASE_URL: ${PUBLIC_BASE_URL}
    volumes: [ "../certs/upay-local-ca.crt:/app/upay-local-ca.crt:ro" ]
    depends_on: [ db ]
  frontend:
    build: ../frontend
  caddy:
    image: caddy:2
    ports: [ "443:443" ]
    volumes: [ "./Caddyfile:/etc/caddy/Caddyfile" ]
volumes: { pgdata: {} }
```
> 测试环境需让 `backend` 容器经 NetBird 访问 `api.upay.local`（共享宿主网络或容器内装 NetBird）。

`deploy/Caddyfile`：
```
{$PUBLIC_DOMAIN} {
    handle /api/* { reverse_proxy backend:8080 }
    handle      { reverse_proxy frontend:80 }
}
```
回调地址 `https://{$PUBLIC_DOMAIN}/api/webhooks/upay` 在 UPay 商户后台登记。

---

## 10. 测试策略

| 层 | 内容 |
|---|---|
| 单元 | **验签**（正确签名/错误签名/超 5 分钟/双签兼容/原始字节）、叠加规则、金额格式化 |
| 集成 | 下单→建单→attach；回调→去重→开通；轮询发现 EXPIRED/FAILED；幂等重复回调只开通一次 |
| 契约 | 复用 `upay-qa` Newman 套件（`npm run lifecycle`）核对 Create/Get 字段 |
| E2E | 注册→登录→下单→（mock/sandbox 收银台）→回调→会员生效；非会员 403 |

验签单测应包含一条**真实回调样本**核对密钥口径（见 6.2 注）。

---

## 11. 开发里程碑（建议）

1. **M1 地基**：迁移 + sqlc + 账号体系（注册/登录/JWT）+ 中间件。
2. **M2 支付主链路**：UPay 客户端 + 下单 + 收银台页 + 回调验签 + 会员开通。
3. **M3 兜底与一致性**：轮询任务 + 订单超时/失败 + 幂等/并发用例。
4. **M4 管理界面**：订阅页/个人中心/当前订阅/订单列表/详情。
5. **M5 联调上线**：UPay 后台配回调端点取 `signing_secret`、验签样本核对、iframe 内嵌实测、部署。

---

## 12. 风险与未决

| 风险 | 应对 |
|---|---|
| 验签密钥口径（是否剥 `whsec_`/解码） | 上线前用真实回调样本核对（6.2 注），单测固化 |
| 收银台不允许 iframe 内嵌 | 已设计降级整页跳转；M5 实测确认 |
| 容器经 NetBird 访问 UPay | 共享宿主隧道或容器内 NetBird；CI 同理 |
| USDT/USD 汇率 | 当前按 1:1 展示「≈USDT」；如需精确换算需 UPay 提供报价（本期不做） |
| 退款/部分退款态 | 本期只读展示，不做用户侧退款入口 |
```

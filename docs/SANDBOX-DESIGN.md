# Sandbox 沙盒模拟模块 — 技术设计

> 目标：在 chl-demo 中为商户技术团队提供"模拟订单到账 + AML 自动审批 + webhook 投递查看"功能。
> 不修改 merchant-admin / merchant-admin-fronted / 生产环境。

---

## 功能概述

1. 商户用 merchant-admin JWT（粘贴方式，Option B；预留 Option C SSO 空间）登录沙盒入口
2. 输入 `payment_request_id` + 场景参数触发模拟到账（调 admin BFF，Dolos 凭据）
3. RISK 场景会产生 FROZEN 充值记录 → 进入 AML 审核；后台 AMLPoller 每分钟自动审批
4. 前端展示模拟单状态流转 + webhook 投递记录

---

## 接口清单

| 用途 | Method | URL |
|------|--------|-----|
| 验证商户 JWT + 取 merchant_id | GET | `https://merchant.upay-test.com/api/v1/auth/profile` |
| 触发模拟到账 | POST | `https://admin.upay-test.com/api/v1/platform/payment-requests/:id/simulate-settlement` |
| 查充值记录（by to_address） | POST | `https://admin.upay-test.com/api/v1/platform/deposits/list` body: `{"to_address":"...","limit":"20","offset":"0"}` |
| 查 AML ticket（by inspection_id） | GET | `https://admin.upay-test.com/api/v1/platform/compliance/aml/review-tickets?inspection_id=:id` |
| 提交 AML 审核通过 | POST | `https://admin.upay-test.com/api/v1/platform/compliance/aml/review-tickets/:uuid/submit-review` |
| 查 webhook 投递记录 | GET | `https://admin.upay-test.com/api/v1/platform/payment-requests/:id/webhooks?page=1&size=20` |
| Dolos 登录 | POST | `https://admin.upay-test.com/api/v1/auth/login` body: `{"email":"...","password":"...","audience":"platform"}` |
| Dolos token 刷新 | POST | `https://admin.upay-test.com/api/v1/auth/refresh` Cookie: `upay_refresh=<token>` |

---

## Simulate 请求体格式

```json
{
  "scenario": "NORMAL|BATCH|OVERPAID|RISK",
  "events": [
    { "amount": "20.000000", "status": "COMPLETED|FROZEN", "delay_ms": 0, "note": "" }
  ]
}
```

RISK 场景用 `status: "FROZEN"`，AML 自动审批链路才会触发。

---

## AML 匹配链路

```
商户提交: payment_request_id + to_address (收银台充值地址) + amount + coin
  └─ chl-demo 存 sandbox_simulations 记录
  └─ POST simulate-settlement (Dolos token)

AMLPoller 每分钟:
  POST /platform/deposits/list {to_address}
    └─ 得到 N 条充值记录，各有 inspection_id
  GET /platform/compliance/aml/review-tickets?inspection_id=:id
    └─ 得到 ticket UUID
  POST /platform/compliance/aml/review-tickets/:uuid/submit-review
    └─ 审批通过 → 标记 approved_at

所有 ticket approved → simulation 状态 → AML_APPROVED
```

---

## 数据库结构（新增）

### `sandbox_simulations`

```sql
id                 bigserial primary key
merchant_id        text not null          -- 从 merchant-admin JWT claims 取
payment_request_id text not null
scenario           text not null          -- NORMAL | BATCH | OVERPAID | RISK
to_address         text                   -- RISK 场景必填（充值地址）
amount             text not null          -- 模拟金额
coin               text                   -- 币种，如 USDT
sim_events         jsonb not null default '[]'  -- 原始 events 参数（透传给 simulate API）
status             text not null default 'SIMULATING'
  -- SIMULATING → AML_PENDING → AML_APPROVED → DONE | FAILED
retry_count        int not null default 0
max_retries        int not null default 20
error_detail       text
created_at         timestamptz not null default now()
updated_at         timestamptz not null default now()
```

### `sandbox_aml_tickets`（一对多，处理多笔充值）

```sql
id              bigserial primary key
simulation_id   bigint not null references sandbox_simulations(id)
inspection_id   text not null unique
ticket_uuid     text                    -- 由 AMLPoller 发现后填入
approved_at     timestamptz
created_at      timestamptz not null default now()
```

---

## 新增 Env Vars

```
DOLOS_EMAIL=admin@upay.local
DOLOS_PASSWORD=password
ADMIN_BASE_URL=https://admin.upay-test.com/api/v1
MERCHANT_ADMIN_BASE_URL=https://merchant.upay-test.com/api/v1
SANDBOX_MAX_AML_RETRIES=20
```

---

## 后端文件清单

```
backend/
├── db/migrations/
│   ├── 0008_sandbox.up.sql
│   └── 0008_sandbox.down.sql
├── internal/
│   ├── config/config.go          ← 新增 5 个 env var
│   ├── admin/
│   │   ├── client.go             ← Dolos 登录/刷新 + 所有 admin API 调用
│   │   └── merchant_auth.go      ← 验证 merchant-admin JWT
│   ├── repo/
│   │   └── sandbox.go            ← sandbox 表的 CRUD
│   ├── handler/
│   │   └── sandbox.go            ← HTTP handlers（POST simulate, GET list/detail）
│   └── job/
│       └── aml_poller.go         ← 每分钟轮询，最大重试 max_retries
└── cmd/server/main.go            ← 注册 sandbox 路由 + 启动 AMLPoller
```

### 后端路由

```
POST /api/sandbox/simulations           → 触发模拟（需 X-Merchant-Token）
GET  /api/sandbox/simulations           → 列表（当前商户，分页）
GET  /api/sandbox/simulations/:id       → 详情 + AML tickets + webhook 投递记录
```

### Dolos AdminClient token 策略

- 内存缓存 access token + `access_expires_at` + refresh cookie
- 每次请求前：剩余 < 5 分钟 → 先 refresh
- 遇 401 → refresh 一次重试 → 再失败则重新 login

---

## 前端文件清单

```
frontend/src/
├── auth/
│   └── SandboxAuthContext.tsx     ← 存 merchantAdminJWT (sessionStorage)；预留 SSO 切口
├── components/
│   └── SandboxLayout.tsx          ← 独立导航，与消费者 Layout 完全隔离
└── pages/sandbox/
    ├── SandboxLogin.tsx           ← 粘贴 merchant-admin JWT
    ├── SimulationList.tsx         ← 模拟单列表（状态 badge）
    ├── SimulationNew.tsx          ← 创建模拟（场景 tab + 参数表单）
    └── SimulationDetail.tsx       ← 单据详情 + webhook 投递记录（5s 轮询到 DONE）
```

### 前端路由（新增到 App.tsx）

```
/sandbox              → redirect /sandbox/simulations
/sandbox/login        → SandboxLogin（未登录时自动跳转）
/sandbox/simulations  → SimulationList
/sandbox/simulations/new → SimulationNew
/sandbox/simulations/:id → SimulationDetail
```

### SandboxAuthContext 说明

- 商户粘贴 JWT → 存 `sessionStorage.sandbox_token`
- 所有 `/api/sandbox/` 请求加 `X-Merchant-Token: <jwt>`
- 独立于消费者的 `AuthContext`，互不干扰
- SSO 将来只替换这层 context 的 token 获取方式

---

## 状态机

```
SIMULATING ──(RISK+deposits found)──→ AML_PENDING
           ──(非RISK/立即完成)──────→ DONE

AML_PENDING ──(所有ticket approved)→ AML_APPROVED → DONE
            ──(retry>=max_retries)──→ FAILED

任意状态 ──(admin API error)──────→ FAILED
```

---

## 不动的部分

- 现有 `reconcile.go`、`webhook.go`、消费者订阅流程 — 完全不改
- `merchant-admin` / `merchant-admin-fronted` — 完全不改
- 生产环境：`DOLOS_EMAIL` 不配则 sandbox 接口返回 503（同现有 UPay 占位模式）

---

## TODO 施工清单

### 后端

- [x] `db/migrations/0008_sandbox.up.sql` — 建 sandbox_simulations + sandbox_aml_tickets 表
- [x] `db/migrations/0008_sandbox.down.sql` — 回滚
- [x] `internal/config/config.go` — 新增 DolosEmail/DolosPassword/AdminBaseURL/MerchantAdminBaseURL/SandboxMaxAMLRetries
- [x] `internal/admin/client.go` — AdminClient（登录/刷新/模拟/充值记录/AML/webhook）
- [x] `internal/admin/merchant_auth.go` — VerifyMerchantToken（调 /auth/profile）
- [x] `internal/repo/sandbox.go` — CreateSimulation / ListSimulations / GetSimulation / ListPendingSimulations / UpsertAMLTicket / ListAMLTickets / MarkSimulationFailed / MarkSimulationDone
- [x] `internal/handler/sandbox.go` — POST /simulate, GET /list, GET /:id
- [x] `internal/job/aml_poller.go` — AMLPoller（tick 逻辑 + 最大重试）
- [x] `cmd/server/main.go` — 接入 AdminClient / MerchantAuth / SandboxHandler / AMLPoller
- [x] `deploy/.env.example` — 补充 5 个新 env var

### 前端

- [x] `src/auth/SandboxAuthContext.tsx`
- [x] `src/components/SandboxLayout.tsx`
- [x] `src/api/sandboxClient.ts`
- [x] `src/pages/sandbox/SandboxLogin.tsx`
- [x] `src/pages/sandbox/SimulationList.tsx`
- [x] `src/pages/sandbox/SimulationNew.tsx`
- [x] `src/pages/sandbox/SimulationDetail.tsx`
- [x] `src/App.tsx` — 注册 /sandbox/* 路由
- [x] `tsconfig.json` — 补充 vite/client types（修复 import.meta.env 类型报错）

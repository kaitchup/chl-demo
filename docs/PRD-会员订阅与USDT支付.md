# 语言学习平台 · 会员订阅与 USDT 支付 需求文档（PRD）

| 项 | 内容 |
|---|---|
| 文档版本 | v0.2 |
| 创建日期 | 2026-06-01 |
| 作者 | Claude Code（与产品负责人共同梳理） |
| 状态 | 待评审 |
| 适用范围 | 前端页面 + 后端服务 + UPay 支付渠道集成 |
| 接口依据 | UPay Acquiring API（`portal.upay.local`，spec 2026-05-04），已据实际文档核对 |

> v0.2 已用 UPay 真实接口文档核对全部集成细节，并采纳全部默认建议（原 §15 待确认项已定稿，见 §15）。
> ⚠️ 重要修正：UPay **按法币计价**（USD/CNY/EUR/HKD/SGD），USDT 仅为**链上结算资产**；回调**只在链上到账时推送**，超时/失败不发回调。详见 §9。

---

## 1. 背景与目标

### 1.1 背景
平台提供多语种在线学习资源（英语、日语、阿拉伯语、韩语、俄语等）。目前缺乏付费变现手段，且无法币收款能力。已接入第三方加密货币收单网关 **UPay**：商户后端创建支付订单（**以法币计价**）→ 用户在 UPay 托管收银台用 **USDT** 支付 → 链上确认后 UPay 回调通知商户。本项目目标是让用户用 USDT 订阅会员，解锁平台全部学习资源。

### 1.2 目标
- 用户可在线订阅 **Super 会员**，在 UPay 收银台用 USDT 付款。
- 支付成功后自动开通会员权益，可无限制使用平台学习资源。
- 提供完整会员/订阅/订单界面（订阅页、个人中心、当前订阅、订单列表、订单详情）。
- 支付链路**回调为主 + 主动轮询兜底**，确保不漏单、不错开会员。

### 1.3 非目标（本期不做）
- 自动续费/周期代扣（UPay 为单次收单，无周期代扣，**到期需用户手动再次下单**）。
- 退款自助流程（UPay 支持退款态，但本期不做用户侧退款入口）。
- 法币直付、发票/税务。
- 多会员等级（本期仅 Super 一档，仅周期不同）。
- 团队/家庭共享会员。

---

## 2. 名词术语

| 术语 | 含义 |
|---|---|
| Super 会员 | 平台唯一付费会员档位，开通后可无限制访问全部学习资源 |
| 计费周期 | 一次订阅覆盖的时长：月付 / 季付 / 年付 |
| 计价币种 | UPay 订单的法币计价单位（本项目用 USD）；用户实际支付等值 USDT |
| 订单（Payment Order） | 一次支付请求，对应 UPay 一笔 payment request（`pay_<ULID>`） |
| 订阅（Subscription） | 用户会员有效期记录，由成功订单驱动延长 |
| 收银台（Checkout） | UPay 返回的 `checkout_url`，托管页面，用户在此付 USDT |
| 回调（Webhook） | UPay 在**链上到账**时向商户预登记地址推送的签名事件 |
| 兜底轮询 | 平台主动调用 UPay 查单接口，弥补回调丢失、发现超时/失败 |

---

## 3. 技术栈与总体架构

### 3.1 技术栈（已确定）
- **后端**：Go + Echo + sqlc + golang-migrate
- **前端**：React + Vite + TanStack Query + shadcn/ui + Tailwind
- **数据库**：PostgreSQL（自建）
- **反向代理**：Caddy（自动 HTTPS）
- **部署**：Docker Compose（VDS）

### 3.2 模块划分
```
chl_demo/
├── backend/                 # Go 服务
│   ├── cmd/server/          # 入口
│   ├── internal/
│   │   ├── handler/         # HTTP handler（Echo）
│   │   ├── service/         # account / membership / payment
│   │   ├── repo/            # sqlc 生成的数据访问层
│   │   ├── upay/            # UPay 客户端 + 回调验签
│   │   └── middleware/      # JWT 鉴权、会员校验
│   └── db/
│       ├── migrations/      # golang-migrate
│       └── queries/         # sqlc .sql
└── frontend/                # React + Vite
    └── src/{pages,components,hooks,api}
```

### 3.3 API 约定（写入 CLAUDE.md）
- 统一返回格式：`{ "data": ..., "error": null }`；错误时 `{ "data": null, "error": { "code": "...", "message": "..." } }`
- 认证：`Authorization: Bearer <JWT>`
- 错误码：HTTP 标准状态码 + 业务错误码字符串
- 金额：内部一律以**字符串/decimal** 表示，禁用浮点（与 UPay 一致，见 §9.3）

---

## 4. 角色与核心用例

| 角色 | 说明 |
|---|---|
| 访客 | 未登录，可浏览营销页，需注册登录后才能下单 |
| 普通用户 | 已注册、无有效会员，学习资源受限 |
| Super 会员 | 有有效订阅，无限制访问学习资源 |
| 系统（定时任务） | 兜底轮询、订单超时、会员到期处理 |

**核心用例**
1. 注册 / 登录 / 登出（账号体系从零搭建）
2. 浏览订阅页，选周期 → 下单 → 跳转/内嵌收银台付 USDT
3. 支付成功 → 自动开通/延长会员
4. 查看当前订阅状态与到期时间
5. 查看历史订单列表与单笔详情
6. 会员到期后访问受限，可再次下单续费

---

## 5. 会员与定价模型

### 5.1 档位
单一档位 **Super**，权益：解锁平台全部语种学习资源，无数量/时长限制。

### 5.2 计费周期与定价（已定稿）
UPay 按法币计价，本项目计价币种取 **USD**；用户在收银台支付等值 USDT（USDT≈1 USD）。

| 周期 | `plan_code` | 时长 | 计价（amount/currency） | 等效月价 | 说明 |
|---|---|---|---|---|---|
| 月付 | `monthly` | 30 天 | `20.00` USD | 20 U | 基准价 |
| 季付 | `quarterly` | 90 天 | `54.00` USD | 18 U | 约 9 折 |
| 年付 | `yearly` | 365 天 | `192.00` USD | 16 U | 约 8 折 |

- 前端展示统一标注「≈ N USDT」，强调用 USDT 支付。
- 价格/折扣/周期天数**存库可配**（`membership_plans` 表），不写死。
- 链/网络（TRC20/ERC20 等）由 UPay 收银台处理，平台侧不指定、不限制。

### 5.3 会员有效期叠加规则
- 用户在**会员有效期内**再次成功下单 → 新周期**叠加到当前到期时间之后**（续期不损失剩余天数）。
- 用户在**会员过期后**下单 → 从**支付完成时刻**起算新周期。
- 公式：`new_expires_at = max(now, current_expires_at) + plan.duration`

---

## 6. 功能需求

### 6.1 账号体系（从零设计）
- **注册**：邮箱 + 密码（bcrypt 加盐存储）。本期不强制邮箱验证。
- **登录**：邮箱 + 密码，签发 JWT（access token，有效期 24h，仅 access，不做 refresh）。
- **登出**：前端清除 token；后端可选维护黑名单。
- **鉴权中间件**：校验 JWT，注入 `user_id`。
- **会员校验中间件**：访问受限资源时校验存在有效订阅（`expires_at > now`）。
- 安全：密码强度校验、登录失败限流、合理 token 过期。

### 6.2 会员订阅与下单
- 展示三种周期及价格（含「≈ N USDT」）。
- 用户选周期 → 后端按 `plan_code` 计算金额（**不信任前端金额**）→ 调 UPay 创建订单 → 返回 `checkout_url` 与平台订单号。
- **重复下单策略（已定稿）**：复用该用户最近一笔未过期且处于 `INITED/AWAITING_PAYMENT` 的订单（避免重复挂单）；无则新建。
- 下单幂等见 §9.2。

### 6.3 支付流程（回调为主 + 轮询兜底）
完整时序见 §10。要点：
1. 后端创建 UPay 订单，落库本地订单（`PENDING`），返回 `checkout_url`、`expires_at`。
2. **收银台呈现（已定稿）**：优先 iframe 内嵌 `checkout_url`；若 UPay 页面 `X-Frame-Options`/CSP `frame-ancestors` 禁止内嵌，则降级为新窗口/整页跳转。**该限制需联调实测**（见 §15 注）。下单时传 `success_url`/`cancel_url` 指回平台支付结果页。
3. **回调（主）**：UPay 在**链上到账**时推送 `payment_order.paid`（足额/超付）或 `payment_order.partially_paid`（未付满）→ 验签 → 幂等 → 仅 `paid` 触发开通/延长会员。
4. **轮询（兜底）**：
   - 前端：支付页轮询本地订单状态接口（每 3~5s），驱动 UI。
   - 后端定时任务：扫描 `PENDING` 订单调 UPay 查单接口对齐状态。⚠️ **超时（EXPIRED）与失败（FAILED）UPay 不发回调，只能靠此轮询发现**。
5. **超时处理**：订单过 `expires_at` 仍未足额 → 经轮询置 `EXPIRED`。

### 6.4 个人中心
- 用户基本信息（邮箱、注册时间、头像占位）。
- 当前会员徽标（Super / 普通）、到期时间。
- 入口：当前订阅、订单列表、修改密码、登出。

### 6.5 当前订阅页
- 展示：档位、状态（生效中/已过期/无）、生效起止、剩余天数。
- 操作：续费（跳订阅页下单）。

### 6.6 订单列表
- 分页展示当前用户全部订单。
- 字段：平台订单号、周期、金额（USD）、状态、创建时间。
- 按状态筛选（全部/待支付/已支付/已超时/失败）。
- 点击进入详情。

### 6.7 订单详情
- 展示：平台订单号、UPay 订单号（`pay_...`）、周期、金额/币种、状态、已到账金额、创建时间、支付时间、收银台过期时间；失败时展示 `failure_code`。
- 待支付订单提供「继续支付」（重开收银台，会话过期则提示重新下单）与「取消订单」。
- 已支付订单展示其开通的会员周期。

### 6.8 资源访问控制
- 学习资源接口统一经会员校验中间件。
- 非会员访问受限资源 → `403 membership_required`，前端引导至订阅页。
- 本期**全部学习资源均需会员**，无免费/试看白名单（如需可后续配置）。

---

## 7. 前端页面清单

| 页面 | 路由（建议） | 关键内容 |
|---|---|---|
| 注册 | `/register` | 邮箱、密码、确认密码 |
| 登录 | `/login` | 邮箱、密码 |
| 会员订阅页 | `/membership` | Super 权益 + 月/季/年周期卡片（标注 ≈USDT）+ 下单 |
| 支付/收银台页 | `/checkout/:orderId` | 内嵌/跳转 UPay `checkout_url` + 状态轮询 + 倒计时（到 `expires_at`） |
| 支付结果页 | `/checkout/:orderId/result` | 成功/失败/超时提示，跳当前订阅 |
| 个人中心 | `/account` | 用户信息 + 会员徽标 + 各入口 |
| 当前订阅 | `/account/subscription` | 会员状态、到期、续费入口 |
| 订单列表 | `/account/orders` | 分页 + 状态筛选 |
| 订单详情 | `/account/orders/:orderId` | 订单全字段 + 继续支付/取消 |

UI 统一用 shadcn/ui；请求统一经 TanStack Query；鉴权失败统一拦截跳登录。

---

## 8. 后端 API 设计（平台自有接口）

> 统一前缀 `/api`，除注册/登录外均需 JWT。返回体遵循 §3.3。

### 8.1 账号
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/register` | 注册 |
| POST | `/api/auth/login` | 登录，返回 JWT |
| POST | `/api/auth/logout` | 登出 |
| GET | `/api/me` | 当前用户信息 + 会员状态 |

### 8.2 会员与下单
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/plans` | 可售周期与价格 |
| GET | `/api/subscription` | 当前订阅状态 |
| POST | `/api/orders` | 创建订单（入参 `plan_code`） |
| GET | `/api/orders` | 订单列表（分页 + 状态筛选） |
| GET | `/api/orders/:id` | 订单详情 |
| GET | `/api/orders/:id/status` | 轻量状态查询（前端轮询用） |
| POST | `/api/orders/:id/cancel` | 取消待支付订单（同时调 UPay cancel） |

### 8.3 UPay 回调
| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/webhooks/upay` | 接收 UPay 事件，验签 + 幂等处理 |

---

## 9. UPay 集成细节（依据真实接口文档）

> Base URL：`https://api.upay.local`（生产对应 `pay.upay.example.com` 域）。测试环境仅经 **NetBird mesh VPN** 可达，需注入自签 CA（`chl-demo/certs/upay-local-ca.crt`）。商户：`mch_CHL`，已确认支持 USDT 收款。

### 9.1 认证
- `Authorization: Bearer <API Key>`。Key 为三段式：`sk_{live|test}_` 前缀 + 26 字符 `key_id` + `.` + 43 字符 `secret`。
- Scope：创建/取消需 `payments:write`，查询需 `payments:read`。
- 密钥经环境变量注入，**绝不进客户端代码、URL、git**。
- 错误：`api_key_missing`(401) / `api_key_malformed`(401) / `api_key_invalid`(401) / `scope_insufficient`(403)。

### 9.2 创建订单 `POST /v1/payment/request`
**请求头**
- `Idempotency-Key`（必填，≤128）：缺失 → 400 `idempotency_key_required`。推荐派生 `<merchant_order_id>:create`。同 `(api_key, key)` 同请求体永久返回首次响应；不同请求体 → 409 `idempotency_conflict`。

**请求体**（严格 JSON，多余字段 → 400 `unknown_field`）

| 字段 | 必填 | 说明（本项目取值） |
|---|---|---|
| `merchant_order_id` | ✅ | 平台订单号，每商户唯一（≤64）；重复 → 409 `merchant_order_id_reused`。建议 `sub_<ULID>` |
| `amount` | ✅ | 计价金额十进制字符串，如 `"20.00"`（USD 2 位） |
| `currency` | ✅ | `"USD"`（法币代码，可选 USD/CNY/EUR/HKD/SGD） |
| `description` | — | 收银台展示，如 `"Super 会员 · 月付"`（≤512） |
| `expires_in_seconds` | — | 收银台会话有效期，范围 1800~3600；取 **1800**（30 分钟） |
| `success_url` | — | 支付成功跳回平台结果页（live 必须 https；test 允许 http） |
| `cancel_url` | — | 取消/超时跳回平台 |
| `metadata` | — | 透传键值（≤50 key，value≤512），原样回到查单。**存 `user_id`、`plan_code`** 便于回调/对账反查 |

**成功响应 200**
```json
{
  "id": "pay_01hzqwe23m...",        // 平台订单 id，^pay_[26 ULID]$
  "object": "payment",
  "merchant_id": "mch_...",
  "merchant_order_id": "sub_...",
  "amount": "20.00",
  "currency": "USD",
  "status": "INITED",
  "checkout_url": "https://pay.upay.../c/cs_...?t=...",  // 重定向目标，含一次性 token
  "created_at": "2026-05-04T12:34:56Z",
  "expires_at": "2026-05-04T13:04:56Z"
}
```
**其它状态码**：400 参数 / 401 / 403 / 409（幂等冲突或单号重用）/ 413 `body_too_large` / 503 `psp_unavailable`（PSP 全不可用）。

### 9.3 查询订单 `GET /v1/payment/request/{id}`
- Path `id` 为 `pay_<26 ULID>`，原样存储传入。
- 返回完整快照：`id, merchant_id, merchant_order_id, amount, currency, status, description, received_amount, settlement_fee, settlement_amount, failure_code, paid_at, created_at, metadata`。
- 金额均为十进制字符串，按币种小数位解析，**用 decimal 比较，勿做字符串相等**。
- 错误：不存在/非本商户 → 404 `not_found`；无鉴权 → 401。

**订单状态枚举（UPay）**

| status | 含义 |
|---|---|
| `INITED` | 已创建，等待用户进入收银台 |
| `AWAITING_PAYMENT` | 已分配收款地址，等待付款 |
| `PAID` | 已足额收款 ✅ 触发开通会员 |
| `EXPIRED` | 超时未支付（**无回调，靠轮询发现**） |
| `FAILED` | 渠道分配失败，见 `failure_code`（**无回调，靠轮询发现**） |
| `REFUNDED` / `PARTIAL_REFUNDED` | 全额/部分退款 |

`failure_code`：`psp_unavailable` / `risk_rejected` / `onchain_rejected` / `expired_no_address`。

### 9.4 回调（Webhook）
- UPay 仅在**链上到账**时向商户后台预登记的回调地址 POST 推送；**不到账不发事件，超时/失败不发事件**。
- **事件类型**：
  - `payment_order.paid` —— 累计 `received_amount ≥ amount`（足额/超付），**收到即开通会员**。
  - `payment_order.partially_paid` —— `0 < received_amount < amount`（未付满），每笔到账各发一次，**不开通会员**（提示用户补款或等超时）。
- **HTTP 头**：`UPay-Signature: t=<unix秒>,v=<hex签名>`、`UPay-Event-Id`、`UPay-Event-Type`。
- **请求体**：
  ```json
  {
    "id": "evt_01k4...",                  // 事件 id（^evt_[26]$），去重键
    "event": "payment_order.paid",
    "created_at": "...",
    "data": {                              // 订单快照子集（扁平）
      "id": "pay_...", "merchant_order_id": "sub_...",
      "amount": "20.00", "currency": "USD",
      "received_amount": "20.00", "settlement_fee": "...",
      "settlement_amount": "...", "expires_at": "...", "created_at": "..."
    }
  }
  ```

**验签（必做）**
1. 待签数据 = `t 的十进制文本 + "." + 请求体原始字节`（**必须用收到的原始 body 字节，勿反序列化后重编码**）。
2. 期望签名 = `lowercase_hex( HMAC-SHA256(signing_secret, 待签数据) )`，与头里 `v=` **常量时间比较**（`hmac.Equal`）。
3. 防重放：头里 `t` 与本机时间相差 **> 5 分钟** → 拒绝。
4. **双签轮换**：密钥轮换后 24h 双签期内 `UPay-Signature` 含两个 `v=`（前新后旧），任一验过即接受。
5. `signing_secret`（`whsec_` 前缀 + 43 字符）在商户后台配置回调端点时获取，仅显示一次；经环境变量注入。

**接收与投递**
1. 先验签，不过直接丢弃。
2. 尽快返回 **2xx**（10s 内，否则重试）；平台只看状态码、忽略响应体。
3. 按 `event.id` 幂等去重（投递至少一次，可能重复、可能乱序）。
4. 投递重试：指数退避 `30s × 2^(n-1)`（上限 8h），约 12 次 / 累计约 1 天后停投。

> 处理建议：回调到达后，以 `data.id` 反查 §9.3 拿权威状态再开通，避免仅凭回调体判断。

### 9.5 取消订单
- 取消待支付订单调 UPay cancel 端点（`payments:write`），本地置 `CANCELED`。

### 9.6 对账兜底（定时任务）
- 周期（如每 1 分钟）扫描本地 `PENDING` 订单：
  - 调 §9.3 对齐状态；`PAID` → 补开通（幂等）；`EXPIRED`/`FAILED` → 置对应终态。
  - 已 `PAID` 但会员未开通（开通环节异常）→ 重试，保证最终一致。

---

## 10. 状态机与时序

### 10.1 支付时序
```
用户            前端                后端                      UPay
 | 选周期下单 -> |                    |                         |
 |              | POST /api/orders ->|                         |
 |              |                    | 建本地订单 PENDING        |
 |              |                    | POST /v1/payment/request(Idempotency-Key) -> |
 |              |                    | <- pay_id, checkout_url, expires_at |
 |              | <- order+checkout  |                         |
 | 收银台付 USDT ----------------------------------------> 托管收银台 |
 |              | 轮询 /status ----->| (查本地状态)              |
 |              |                    | <-- 回调 paid(主, 链上到账) -- UPay |
 |              |                    | 验签+幂等+反查+开通会员    |
 |              |                    | 定时轮询(兜底, 含超时/失败) -> GET /v1/payment/request/{id} |
 | 看到成功      | <- PAID            |                         |
```

### 10.2 本地订单状态机（映射 UPay）
```
PENDING ──UPay PAID(回调/轮询)──> PAID ──(触发一次)──> 开通/延长会员
   │
   ├──用户取消(+UPay cancel)──> CANCELED
   ├──UPay EXPIRED(仅轮询)────> EXPIRED
   └──UPay FAILED(仅轮询)─────> FAILED
```
- 本地 `PENDING` 聚合 UPay 的 `INITED`/`AWAITING_PAYMENT`/`partially_paid`（未付满）。
- 终态：`PAID`/`EXPIRED`/`CANCELED`/`FAILED`，不可逆。
- 仅 `PAID` 触发会员变更，且对单笔订单**只触发一次**（`applied_to_subscription` 幂等）。
- `partially_paid`：保持 `PENDING`，前端提示补款，超时则随 `EXPIRED` 终结。

### 10.3 订阅状态
- `none`（从无会员）/ `active`（`expires_at > now`）/ `expired`。
- 由成功订单按 §5.3 更新 `expires_at`。

---

## 11. 数据模型（PostgreSQL，建议）

```sql
-- 用户
users(
  id BIGSERIAL PK,
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now()
)

-- 计费周期/套餐（可配）
membership_plans(
  code TEXT PK,                 -- 'monthly' | 'quarterly' | 'yearly'
  name TEXT NOT NULL,
  duration_days INT NOT NULL,
  amount NUMERIC(18,2) NOT NULL,   -- 计价金额
  currency TEXT NOT NULL DEFAULT 'USD',
  active BOOLEAN DEFAULT true
)

-- 订阅（每用户一条当前订阅）
subscriptions(
  id BIGSERIAL PK,
  user_id BIGINT FK -> users UNIQUE,
  tier TEXT DEFAULT 'super',
  expires_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ DEFAULT now()
)

-- 订单
orders(
  id BIGSERIAL PK,
  merchant_order_id TEXT UNIQUE NOT NULL,  -- 提交给 UPay 的唯一单号 sub_<ULID>
  upay_payment_id TEXT UNIQUE,             -- pay_<ULID>
  user_id BIGINT FK -> users,
  plan_code TEXT FK -> membership_plans,
  amount NUMERIC(18,2) NOT NULL,           -- 计价金额
  currency TEXT NOT NULL DEFAULT 'USD',
  status TEXT NOT NULL,                     -- PENDING/PAID/EXPIRED/CANCELED/FAILED
  received_amount NUMERIC(18,2) DEFAULT 0,  -- 已到账（轮询/回调更新）
  failure_code TEXT,                        -- UPay FAILED 时
  checkout_url TEXT,
  idempotency_key TEXT,
  created_at TIMESTAMPTZ DEFAULT now(),
  paid_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,                   -- UPay 收银台过期时间
  applied_to_subscription BOOLEAN DEFAULT false  -- 是否已开通会员，防重复
)

-- 回调事件（幂等去重 + 审计）
webhook_events(
  id BIGSERIAL PK,
  upay_event_id TEXT UNIQUE NOT NULL,       -- evt_<ULID>
  upay_payment_id TEXT,
  event_type TEXT,                          -- payment_order.paid / .partially_paid
  payload JSONB,
  signature_valid BOOLEAN,
  received_at TIMESTAMPTZ DEFAULT now(),
  processed BOOLEAN DEFAULT false
)
```

---

## 12. 安全、合规与运维

- **密钥管理**：UPay API Key、回调 `signing_secret`（`whsec_`）、JWT 签名密钥经环境变量/密管注入，不入库不入仓。
- **回调安全**：强制 HMAC-SHA256 验签（原始字节）+ `t` 5 分钟防重放 + `event.id` 幂等 + 双签期兼容；不依赖前端可篡改参数判断支付结果，一律以 UPay 服务端数据为准。
- **金额防篡改**：下单金额由后端按 `plan_code` 计算，不信任前端。
- **幂等三处**：创建订单（Idempotency-Key）、回调处理（event.id）、会员开通（applied_to_subscription）。
- **并发**：会员到期时间更新加行锁/乐观锁，避免并发下单叠加错误。
- **审计**：订单流转、会员开通、回调全量留痕（webhook_events）。
- **限流**：登录/注册/下单接口限流防刷。
- **网络依赖**：测试环境 UPay 仅经 NetBird 可达；服务/CI 需保证隧道在线，否则表现为「网关不可达」。
- **TLS**：UPay 要求 TLS 1.2+ AEAD；客户端严格校验主机名 + 证书链。

---

## 13. 非功能需求

| 维度 | 要求 |
|---|---|
| 性能 | 下单 P95 < 2s（含 UPay 调用，NetBird+TLS ~0.8~1.1s）；列表查询 P95 < 300ms |
| 可用性 | 回调丢失/超时/失败由轮询兜底，最终一致；目标不漏开/错开会员 |
| 可观测 | 订单与回调关键路径日志 + 指标（下单数、成功率、回调成功率、轮询修复数、验签失败数） |
| 兼容 | 收银台兼容主流桌面/移动浏览器 |
| 国际化 | 本期不做前端 i18n，预留文案结构 |

---

## 14. 验收标准（关键场景）

1. 注册→登录→选月付下单→收银台付 USDT→**回调 paid 到达**→会员变 Super，到期 = now + 30 天。
2. 同场景**回调丢失**，轮询兜底在 ≤ 2 分钟内置 PAID 并开通会员。
3. 会员有效期内续费季付：到期 +90 天（不损失剩余天数）。
4. 订单过 `expires_at` 未支付 → 轮询置 EXPIRED，会员不变（验证无回调路径下仍能终结）。
5. 同一 `event.id` 重复回调 / 重复轮询命中：会员只开通一次，到期不重复叠加。
6. `partially_paid`（未付满）：不开通会员，订单保持 PENDING，前端提示补款。
7. 非会员访问受限资源 → 403 引导订阅；会员访问正常。
8. 前端篡改金额无效，实际下单金额以后端套餐价为准。
9. 回调验签：错误签名 / `t` 超 5 分钟 → 拒绝并不开通。

---

## 15. 设计决策（原待确认项，已采纳默认定稿）

| # | 事项 | 定稿结论 |
|---|---|---|
| 1 | 季付/年付定价 | 季付 54 USD（9折）、年付 192 USD（8折），月付 20 USD |
| 2 | 计价币种 / 网络 | 计价 USD，用户付等值 USDT；不限制链/网络（由收银台处理） |
| 3 | 注册是否邮箱验证 | 本期不强制 |
| 4 | JWT 形态 | 仅 access token，有效期 24h |
| 5 | 重复下单策略 | 复用最近未过期 `INITED/AWAITING_PAYMENT` 订单 |
| 6 | 免费/试看资源 | 暂无，全部资源需会员 |
| 7 | 回调验签方案 | 采用文档方案：HMAC-SHA256(原始字节)、`UPay-Signature: t=,v=`、5 分钟防重放、event.id 幂等、双签兼容（详见 §9.4） |
| 8 | 前端 i18n | 本期不做，预留文案结构 |
| 9 | 收银台呈现 | iframe 优先，被 CSP/X-Frame-Options 拦截则降级跳转 —— **需联调实测确认是否允许内嵌**（唯一遗留待验证项） |

---

## 16. 后续步骤
1. 业务方评审本文档（v0.2 已据真实接口定稿，仅余 §15#9 iframe 内嵌待联调实测）。
2. 在 UPay 商户后台为 `mch_CHL` 配置回调端点并取得 `signing_secret`；创建 `sk_test_` 密钥（scope: payments:read + payments:write）。
3. 进入技术方案设计：数据库迁移、API 详设、UPay 客户端 + 验签实现、前端原型。

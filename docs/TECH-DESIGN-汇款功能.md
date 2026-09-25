# 汇款（Payout）功能 — 技术设计

> 版本 v0.1 · 2026-09-25 · 状态：设计定稿，可开工
> 需求 / 决策 / 物料：[PRD-汇款功能-可行性分析.md](PRD-汇款功能-可行性分析.md)（本文不重复其中的决策理由）
> 设计稿截图：[assets/payout/](assets/payout/) · 真实动态表单存档：[assets/payout/forms/](assets/payout/forms/)
> UPay 接口：《Upay OpenAPI Payout 接口文档》v2 + <https://upay-api.readme.io>

## 0. 测试环境实测结论（2026-09-25，商户 10103 puppy）

本设计的关键假设都已在测试环境用真实凭据验证过（探测脚本不入仓）：

| # | 事实 | 结论 / 对设计的影响 |
|---|---|---|
| 1 | 签名 `METHOD\|URI\|TS\|REQID\|SIGN_PARAM`，HMAC-SHA256(SecretKey) → Base64 | ✅ 可用。签错返回 `1005` |
| 2 | **加密接口签的是明文参数**（非 `payload=<jwe>`） | ✅ 已定。签 payload 返回 `1005` |
| 3 | JWE：RSA-OAEP-256 + A128GCM + `zip=DEF`，`kid=sha256(平台公钥PEM)` hex | ✅ go-jose v4 加解密均通过 |
| 4 | 失败时 HTTP 仍是 200，靠 `code` 判断；`success` 字段不可靠 | 客户端只认 `code == 0` |
| 5 | IP 白名单当前未启用（本机 IP 可调） | 部署后建议在后台配 VPS IP |
| 6 | `debit/coin/list` → USDT `coinId="458884"`，USDC `"458888"` | **`debitCoinId` 传数字 ID**，文档示例 `"USDT"` 是错的；启动时按 symbol 查表 |
| 7 | `fileType=10` 来自 `payer/dynamic/form` 里 `proofImage` / `materiaImage` 的 `description`（"Upload files via the /v1/common/file/upload endpoint, fileType=10"）。实测上传 **`fileType=10` 对 jpg/png/pdf 一律报 `10124`**；**`fileType=37` 报 `10301 Parameter 'fileType' value error`**；1–9 均可上传 | ✅ **根因**：`AgentFileTypeEnum.PAYOUT(10)` 允许格式为空列表；37 是内部存储路径枚举。**UPay 已修复（09-25 13:27 复测）：传 `fileType=10`，jpg/png/pdf 均可，`payer/add` 接受** |
| 8 | 上传需给 multipart 分段设正确 `Content-Type`；**上传接口不加密，签名 `SIGN_PARAM` 为空** | 客户端实现注意 |
| 9 | 表单日期字段自相矛盾：`format: "yyyy-MM-dd"`，`description: "Unix timestamp in milliseconds (ms)"`（`payer` / `beneficiary` 表单均如此）。**实测只接受毫秒时间戳字符串**：传 `yyyy-MM-dd` 时 `payer/add`、`beneficiary/add` 均返回 `8002 … Only a millisecond timestamp is accepted.` | ✅ 按 ms 实现（前端选日期 → 后端转 UTC 0 点毫秒）；`format` 字段忽略 |
| 10 | `bank/account/add` **不传 IBAN 报 `8002 IBAN is required`**；IBAN 与国家不做一致性校验 | IBAN 前端必填（见 §7.3 注） |
| 11 | 汇款金额范围 **5 ~ 100000**（`8009`），与文档的 0.01 不同 | 前端 / 后端校验 ≥ 5 |
| 12 | `order/creation` 返回 `orderStatus=0`（文档无此值）；**~5 秒内**变 `2`，出 1 条报价 | 0 视同「报价中」 |
| 13 | 报价 `validUntil - crateAt = 180 秒`（与设计稿 03:00 一致） | 倒计时取 `validUntil` |
| 14 | **过期 30 秒后（仅调 `quote/info`）报价状态仍是 `3=可用`，没有新报价、订单没变 12**。22 个接口里没有「重新报价」接口，但状态 `12=待确认新报价`、报价状态 `6=已替换` 说明存在重报价机制 | **重报价入口待 UPay 提供**；拿到前以「取消 + 重新下单」兜底，见 §6.3 |
| 15 | 时间字段是**毫秒时间戳字符串**（`crateAt:"1790318127000"`），非文档里的 ISO 串；`quoteTime` 比实际快 8 小时 | 统一按 ms 解析；不用 `quoteTime` |
| 16 | 报价里有文档未列字段：`channelCurrency`、`channelTotalAmount`、`batchNo`、`quoteId`、`kycUrl`；`remitMethod` 是字符串 `"swift"`（文档写 int） | 解析宽松；`kycUrl` 非空 → 引导 KYC |
| 17 | 20 USD 报价：`debitAmount=68.06`、`fixedFee=10`、`transactionFee=3`、`exchangeFee=0` | 「付款总额」= `debitAmount`；「支付金额」= `debitAmount − 三项费` |
| 18 | 取消后 `order/info` 的 `debitInfo` 全部归零；商户余额无变化 | 本地保存确认时的报价快照用于展示 |
| 19 | 新建的 payer / 银行账户 `dataCompleteStatus=2`（部分缺失），但下单、报价不受影响 | 仅展示，不阻塞 |

测试环境已留下的数据（可复用联调）：payer `2103372212922290176`、beneficiary `2103372347110658048`、bankAccount `2103372468649005056`、已取消订单 `PO1790318127PROBE`。

## 1. 设计原则

沿用 [TECH-DESIGN-技术方案.md](TECH-DESIGN-技术方案.md) §1 的约定，额外强调：

- **金额全程字符串 / decimal**，下单金额只接受 `^\d+(\.\d{1,2})?$`；
- **UPay 是个人资料的权威来源**：本地只存编号 + 展示摘要，详情实时查；
- **所有 `xxxNo` 先查本地归属再转发**，UPay 侧数据全挂在商户名下，没有用户维度；
- **状态以 `order/info` 回查为准**，回调只作为「该去查了」的信号；
- 与现有收单（`internal/upay`）**完全隔离**：不同包、不同配置、不同表。

## 2. 工程结构（新增 / 改动）

```
backend/
├── internal/upayopen/            # 新包：UPay OpenAPI（签名 + JWE + Payout 接口）
│   ├── client.go                 # Client、do()、错误类型
│   ├── crypto.go                 # 签名、JWE 加解密、key 解析
│   ├── payout.go                 # 22 个 Payout 接口 + 文件上传 + merchant/asset
│   ├── formdata.go               # formData 构造器（group/fields/多组）
│   ├── webhook.go                # 回调解密 + 验签
│   └── *_test.go
├── internal/handler/payout.go    # /api/payout/* handler
├── internal/handler/payout_webhook.go
├── internal/repo/payout.go       # payout_* 表读写
├── internal/service/payout.go    # 下单 / 确认 / 状态同步（handler、job、webhook 共用）
├── internal/job/payout_sync.go   # 轮询兜底
├── internal/middleware/payout.go # payout_enabled 校验
└── db/migrations/0013_payout.{up,down}.sql

frontend/src/
├── api/payout.ts                 # 类型 + 请求函数
├── components/PayoutLayout.tsx   # max-w-md 移动端容器 + 顶栏（← / ×）
├── components/payout/            # PinSheet（交易密码键盘）、Stepper、CountryPicker、ImageUpload、Countdown
└── pages/payout/                 # 见 §8
```

## 3. 配置与环境变量

变量名沿用 UPay 给的 `UPA_*`：

| 变量 | 必填 | 说明 |
|---|---|---|
| `UPA_HOST` | 是 | `https://openapi.upay-test.best` |
| `UPA_API_KEY` | 是 | `X-UPA-APIKEY` |
| `UPA_SECRET_KEY` | 是 | 请求签名 + 回调验签（默认同一把，见 §6.4 待确认） |
| `UPA_PLATFORM_PUBLIC_KEY` | 是 | 平台公钥，**纯 base64 DER**（无 PEM 头尾，代码自行包装） |
| `UPA_MERCHANT_PRIVATE_KEY` | 是 | 商户私钥，PKCS#8 纯 base64 |
| `PAYOUT_DEBIT_SYMBOL` | 否 | 默认 `USDT`；启动时经 `debit/coin/list` 解析成 `coinId` |
| `PAYOUT_UPLOAD_FILE_TYPE` | 否 | 默认 `10`（UPay 已修复；保留配置项以防回归） |
| `PAYOUT_SYNC_INTERVAL` | 否 | 默认 `60s` |

- `cfg.PayoutEnabled()` = `UPA_API_KEY != "" && UPA_HOST != ""`；未配置时不注册 `/api/payout/*` 路由、`/api/me` 返回 `payout_enabled=false`。
- 密钥只放 `backend/.env` / `deploy/.env`（均已 gitignore）。**当前私钥已在聊天中出现过，上生产前必须重新生成密钥对并在后台替换公钥。**

## 4. 数据库迁移 `0013_payout`

```sql
-- 白名单 + 交易密码
ALTER TABLE users
  ADD COLUMN payout_enabled              BOOLEAN     NOT NULL DEFAULT false,
  ADD COLUMN trade_password_hash         TEXT,
  ADD COLUMN trade_password_fail_count   INT         NOT NULL DEFAULT 0,
  ADD COLUMN trade_password_locked_until TIMESTAMPTZ;

-- 1 用户 = 1 付款人
CREATE TABLE payout_payers (
    user_id     BIGINT PRIMARY KEY REFERENCES users(id),
    payer_no    TEXT   NOT NULL UNIQUE,
    name        TEXT   NOT NULL,            -- 展示用，"名 姓"
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- UI 上的「收款人」= 收款人 + 1 个银行账户
CREATE TABLE payout_recipients (
    id                 BIGSERIAL PRIMARY KEY,
    user_id            BIGINT NOT NULL REFERENCES users(id),
    given_name         TEXT   NOT NULL,
    family_name        TEXT   NOT NULL,
    beneficiary_no     TEXT   UNIQUE,       -- 第 1 步成功后写入
    bank_account_no    TEXT   UNIQUE,       -- 第 2 步成功后写入；NULL = 未完成，可重试
    bank_name          TEXT   NOT NULL,
    swift_code         TEXT   NOT NULL,
    account_last4      TEXT   NOT NULL,     -- 只存后 4 位
    currency           TEXT   NOT NULL,     -- USD
    bank_country       TEXT   NOT NULL,
    last_error         TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON payout_recipients(user_id, created_at DESC);

CREATE TABLE payout_orders (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id),
    recipient_id     BIGINT NOT NULL REFERENCES payout_recipients(id),
    third_order_no   TEXT   NOT NULL UNIQUE,   -- "PO" + ULID
    order_no         TEXT   UNIQUE,            -- UPay 订单号，创建成功后写入
    amount           TEXT   NOT NULL,          -- 汇出法币金额，如 "20.00"
    currency         TEXT   NOT NULL,          -- 到账币种（= 账户币种）
    debit_coin       TEXT   NOT NULL,          -- USDT
    usage            TEXT   NOT NULL,
    note             TEXT   NOT NULL,
    status           INT    NOT NULL DEFAULT 0, -- UPay 订单状态原值（0..12）
    quote            JSONB,                    -- 用户确认时的报价快照（费用明细展示用）
    last_info        JSONB,                    -- 最近一次 order/info 原文
    confirmed_at     TIMESTAMPTZ,
    synced_at        TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON payout_orders(user_id, id DESC);
CREATE INDEX ON payout_orders(status) WHERE status IN (3, 5, 6, 10);

CREATE TABLE payout_order_events (
    id           BIGSERIAL PRIMARY KEY,
    order_id     BIGINT NOT NULL REFERENCES payout_orders(id),
    status       INT    NOT NULL,
    observed_at  TIMESTAMPTZ NOT NULL,
    source       TEXT   NOT NULL,     -- api | poll | webhook
    UNIQUE (order_id, status)          -- 每个状态只记第一次出现
);

-- 回调去重（外层 orderNo = 通知唯一号）
CREATE TABLE payout_webhook_events (
    notify_no    TEXT PRIMARY KEY,
    raw_log_id   BIGINT REFERENCES webhook_raw_log(id),
    order_no     TEXT,                 -- detail.orderNo
    status       INT,                  -- detail.status（仅记录，不直接采信）
    processed_at TIMESTAMPTZ,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

说明：
- 可行性分析里的 `payout_beneficiaries` + `payout_bank_accounts` 合并成 `payout_recipients` 一张表（UI 已定 1:1）。
- 状态用 UPay 原值存，UI 阶段在读取时推导（§6.2），避免两套状态机。
- `payout_order_events` 用 `UNIQUE(order_id, status)` + `ON CONFLICT DO NOTHING`，轮询 / 回调 / 接口三路并发写入天然幂等。

## 5. `internal/upayopen` 客户端

### 5.1 请求流程

```go
type Client struct {
    host, apiKey, secret string
    platformPub *rsa.PublicKey; platformKID string
    merchantPriv *rsa.PrivateKey
    http *http.Client            // Timeout 15s
}

// call: encrypted 接口 → body {"payload": JWE(明文JSON)}；否则 body 为明文 JSON（无参数时为空）。
// 签名永远基于明文参数。
func (c *Client) call(ctx context.Context, uri string, params map[string]any, encrypted bool, out any) error
```

1. `requestID = uuid`，`ts = now.UnixMilli()`
2. `signParam`：顶层 key 升序，`k=v` 用 `&` 连接；值为 string 原样、数字按 JSON 数字、对象/数组按 **紧凑 JSON**（`formData` 本身是字符串，原样）
3. `X-UPA-SIGN = Base64(HMAC-SHA256(secret, "POST|"+uri+"|"+ts+"|"+rid+"|"+signParam))`
4. 响应：`code != 0` → `*APIError{Code, Msg, RequestID}`；加密接口从 `data.payload` 用商户私钥解密后再 `json.Unmarshal` 到 `out`
5. **每次调用记一行日志**：uri、requestID、code、耗时（不打印明文参数，避免 PII 入日志）

**金额签名一致性**：`amount` 用 `json.Number("20.00")` 放进 params，JSON 序列化和签名串都输出 `20.00`，不会出现 `20` / `20.0` 不一致。

### 5.2 JWE

```go
enc, _ := jose.NewEncrypter(jose.A128GCM,
    jose.Recipient{Algorithm: jose.RSA_OAEP_256, Key: platformPub, KeyID: platformKID},
    (&jose.EncrypterOptions{Compression: jose.DEFLATE}).WithType("JOSE"))
// 解密
obj, _ := jose.ParseEncrypted(s, []jose.KeyAlgorithm{jose.RSA_OAEP_256}, []jose.ContentEncryption{jose.A128GCM})
plain, _ := obj.Decrypt(merchantPriv)
```

`platformKID = hex(sha256(PEM 文本))`，PEM 由纯 base64 按 64 列折行包装（与实测一致）。

### 5.3 接口封装

一期用到的方法（其余接口按需再加）：

| 方法 | UPay 接口 | 加密 |
|---|---|---|
| `DebitCoins` | `order/debit/coin/list` | 否 |
| `UploadFile(ctx, r io.Reader, name, mime, fileType)` | `common/file/upload`（multipart，SIGN_PARAM 空） | 否 |
| `AddPayer / PayerInfo` | `payer/add`、`payer/info` | 是 |
| `AddBeneficiary / BeneficiaryInfo` | `beneficiary/add`、`beneficiary/info` | 是 |
| `AddBankAccount / BankAccountInfo` | `bank/account/add`、`bank/account/info` | 是 |
| `CreateOrder / QuoteInfo / ConfirmOrder / CancelOrder / OrderInfo` | `order/*` | 是 |
| `MerchantAsset` | `merchant/asset`（运维自检用） | 否 |

### 5.4 formData 构造器

```go
f := upayopen.NewForm("person").
    Group("base", F("name", "Probe Tester"), F("dateOfBirth", ms), ...).
    MultiGroup("document", 0, F("type", "passport"), ...).   // → "fields": {"0": [...]}
    Group("residentialAddress", ...)
s := f.JSON()   // 紧凑 JSON 字符串，作为 formData
```

**字段以 UPay 动态表单为准，设计稿按表单补齐**（产品已确认）。字段键、分组、选项值以 [assets/payout/forms/](assets/payout/forms/) 为准；**选项值（性别 `Male/Female`、证件类型等）前端下拉直接用存档里的 value**，后端再校验一遍白名单。

## 6. 后端核心流程

### 6.1 付款人（KYC）

```
POST /api/payout/files        multipart(file) → UploadFile(fileType=cfg) → {file_id}
POST /api/payout/payer        6 步向导一次性提交
```

- 已有 payer → `409 PAYER_EXISTS`
- 后端校验必填 / 格式 → 组 formData（字段映射 §7.2）→ `AddPayer` → 写 `payout_payers`
- UPay 返回 `10019/10020`（文件过期）→ `422 FILE_EXPIRED`，前端回到上传步骤
- `GET /api/payout/payer` 返回 `{exists, name}`（不回显证件等资料）

### 6.2 订单状态与 UI 阶段

| UPay status | UI 阶段 `stage` | 终态 | 轮询 |
|---|---|---|---|
| 0, 1 | `quoting` | | 前端 |
| 2, 12 | `awaiting_confirm` | | 前端 |
| 4 | `quote_failed` | ✅ | |
| 3, 5 | `paid`（① 支付已收到） | | 后台 |
| 6 | `processing`（② 银行处理中） | | 后台 |
| 7 | `completed`（③ 转账已转出） | ✅ | |
| 8 | `failed` | ✅ | |
| 9 | `canceled` | ✅ | |
| 10 | `refunding` | | 后台 |
| 11 | `refunded` | ✅ | |

`service.ApplyOrderStatus(order, newStatus, observedAt, source)`：更新 `payout_orders.status` + 插 `payout_order_events`（冲突忽略）。**所有状态写入只走这一个函数。**

### 6.3 下单 → 报价 → 确认

```
POST /api/payout/orders                 {recipient_id, amount, usage, note}
GET  /api/payout/orders/:id/quote       → 代理 QuoteInfo，返回可用报价 + 剩余秒数
POST /api/payout/orders/:id/confirm     {quote_no, trade_password}
POST /api/payout/orders/:id/cancel
```

**创建**：校验归属 + `amount ∈ [5, 100000]`、≤ 2 位小数 → `note` 为空用 `usage` 补 → 先插本地订单（status=0，`third_order_no = "PO"+ULID`）→ `CreateOrder(remitMethod=swift, debitCoinId=启动时解析的 coinId)` → 回写 `order_no`、status。UPay 失败则本地标 `status=4` 并返回错误码给前端。`informationToImprove` 非空时原样返回，前端提示资料不完整。

**报价**：前端每 2 秒调一次 `/quote`；后端取 `quoteList` 中 `status=3` 的一条，返回
`{quote_no, debit_total, pay_amount, fixed_fee, transaction_fee, exchange_fee, fee_currency, destination_amount, destination_currency, expires_in, kyc_url}`，
其中 `pay_amount = debitAmount − fixedFee − transactionFee − exchangeFee`（decimal 计算），`expires_in = validUntil − now`。

**过期（实测 #14）**：
- 前端倒计时到 0 → 禁用「确认汇款」，按钮变「重新获取报价」
- 点击 → `POST /orders/:id/requote`。后端实现二选一：
  - **首选**：调用 UPay 的重报价接口（**待 UPay 提供**），订单号不变，新报价出现后状态 12 → 前端刷新报价
  - **兜底**（接口未提供前）：`CancelOrder(旧单)` + 用同样参数新建订单 → 返回新订单 id，前端跳到新订单的确认页
- 后端 `/confirm` 也校验 `validUntil > now + 5s`，否则 `409 QUOTE_EXPIRED`（防止倒计时偏差）

**确认**：
1. 归属校验、订单状态 ∈ {2, 12}
2. 交易密码校验（§6.5）；未设置 → `428 TRADE_PASSWORD_NOT_SET`
3. 再查一次 `QuoteInfo`，确认 `quote_no` 仍为可用且未过期
4. `ConfirmOrder` → 把报价快照写入 `payout_orders.quote`、`confirmed_at`；status 以返回值 / 随后的 `OrderInfo` 为准（实测文档注明 confirm 可能返回空 data）
5. 触发一次异步 `SyncOrder`

**取消**：仅 status ∈ {0,1,2,12}（UPay 对已进入代付的单返回 `10207`）。

### 6.4 回调 `POST /api/webhooks/upay-payout`

| 步骤 | 说明 |
|---|---|
| 1 | 读原始 body（text/plain JWE 串）+ headers → `SaveRawWebhook(source="upay-busi-payout")` |
| 2 | 商户私钥解密 → `{event, trace, orderNo, timestamp, detail}` |
| 3 | 先解密取 `event`，再验签：`Base64(HMAC-SHA256(UPA_SECRET_KEY, event+"\|"+X-UPA-TIMESTAMP+"\|"+原始 body 密文串))`，常量时间比较；失败 → 401。用报文里的 `event` 而非写死常量，这样 UPay 验证地址时推送的任何已签名事件都能回 `SUCCESS`（已实现：`handler/payout_webhook.go`） |
| 4 | `X-UPA-TIMESTAMP` 与当前时间差 > 30 分钟 → 401（原因见下方「发送端实现」） |
| 5 | `INSERT payout_webhook_events(notify_no=外层 orderNo) ON CONFLICT DO NOTHING`；重复 → 直接回 `SUCCESS` |
| 6 | **立即回 200 + 纯文本 `SUCCESS`** |
| 7 | goroutine（带 10s 超时的独立 ctx）：解析 `detail` → 按 `detail.orderNo` 找本地订单（找不到只记日志）→ `SyncOrder`（调 `OrderInfo`，以其 status 为准）→ event 的 `observed_at` 用推送 `timestamp` → `processed_at=now` |

`processed_at IS NULL` 且超过 1 分钟的回调记录由轮询任务顺带补处理。

**发送端实现（`upay-broker-server` `mq/WebhookConsumer.post:241`，据此确定上表规则）**

```java
String event = String.format("%s_%s", data.getEventType(), data.getEventName()); // "PO" + "_" + "ORDER_STATUS_CHANGE"
payload = {event, trace, orderNo, timestamp: pushTime, detail: pushParams(JSON 字符串)};
String body = new JWECrypto(agentPublicKey, null).encrypt(payload.toJSONString()); // 用商户公钥加密
header X-UPA-REQUESTID = data.getRequestId();                  // 重推复用同一行数据 → requestId / trace 不变
header X-UPA-TIMESTAMP = pushTime;                             // 与 payload.timestamp 相同
header X-UPA-SIGN      = Base64(HmacSHA256(secretKey, event + "|" + timestamp + "|" + body));
Content-Type: text/plain;  连接 / 读超时各 5s;  成功判定: HTTP 200 且响应体 equalsIgnoreCase("SUCCESS")
```

注意：`X-UPA-TIMESTAMP` 是**首次推送时间**，重推不变 —— ±5 分钟防重放窗口会误杀重推（UPay 每 3 秒重试 5 次，总共 15 秒，影响不大；但若 UPay 侧排队延迟超过 5 分钟会被拒）。放宽为 **±30 分钟**，重复由外层 `orderNo` 去重兜底。

### 6.5 交易密码

- `POST /api/payout/trade-password {password}`：仅在未设置时可调；6 位数字；bcrypt 存 `users.trade_password_hash`
- 校验：`locked_until > now` → `423 TRADE_PASSWORD_LOCKED`（返回剩余秒数）；错误 → `fail_count++`，达到 5 次锁 30 分钟并清零；正确 → 清零
- 前端首次确认时：PinSheet 进入「设置」模式（输入两次）→ 调设置接口 → 用同一密码继续 `/confirm`，用户无需再输
- 忘记密码不做：`UPDATE users SET trade_password_hash = NULL ...` 手动重置

### 6.6 轮询兜底 `job/payout_sync.go`

每 `PAYOUT_SYNC_INTERVAL`（60s）：
1. 取 `status IN (3,5,6,10) AND created_at > now() - interval '7 days'` 的订单 → `SyncOrder`
2. 补处理 `payout_webhook_events` 中 `processed_at IS NULL AND received_at < now() - 1min` 的记录
3. 单次最多 50 单，逐单间隔 200ms，避免触发 `900 操作频繁`

### 6.7 中间件

`middleware.PayoutEnabled(repo)`：JWT 之后执行，查 `users.payout_enabled`，false → `403 PAYOUT_NOT_ENABLED`。挂在 `/api/payout` 分组上；webhook 路由不挂。

## 7. 平台 API 契约

统一返回 `{data, error}`（沿用现有约定）。均需 `Authorization: Bearer` + `payout_enabled`。

| 方法 | 路径 | 请求 | 响应 `data` |
|---|---|---|---|
| GET | `/api/payout/bootstrap` | — | `{has_payer, payer_name, has_trade_password, debit_symbol, min_amount:"5", max_amount:"100000"}` |
| GET | `/api/payout/options` | — | 国家列表、区号、证件类型、性别、有效期类型、职业状态、收入来源（来自存档表单 + 静态国家表） |
| POST | `/api/payout/files` | multipart `file`（≤ 4MB，jpg/png/jpeg） | `{file_id}` |
| GET | `/api/payout/payer` | — | `{exists, name}` |
| POST | `/api/payout/payer` | §7.2 | `{name}` |
| GET | `/api/payout/recipients` | — | `[{id, name, swift_code, account_last4, currency, complete}]` |
| POST | `/api/payout/recipients` | §7.3 | `{id, complete}` |
| POST | `/api/payout/recipients/:id/retry` | — | `{id, complete}` |
| GET | `/api/payout/recipients/:id` | — | 摘要 + 实时 `beneficiary/info`、`bank/account/info` 的展示字段 |
| POST | `/api/payout/orders` | `{recipient_id, amount, usage, note}` | `{id, status, stage}` |
| GET | `/api/payout/orders` | `?before_id=&limit=20` | 列表：`{id, amount, currency, debit_total, recipient_name, stage, created_at}` |
| GET | `/api/payout/orders/:id` | — | 详情 + `timeline:[{stage, at}]` + 报价快照 + 收款人展示字段 |
| GET | `/api/payout/orders/:id/quote` | — | §6.3 |
| POST | `/api/payout/orders/:id/confirm` | `{quote_no, trade_password}` | `{status, stage}` |
| POST | `/api/payout/orders/:id/cancel` | — | `{status, stage}` |
| POST | `/api/payout/orders/:id/requote` | — | `{id}`（新订单） |
| POST | `/api/payout/trade-password` | `{password}` | `{}` |

`/api/me` 增加 `payout_enabled`。

错误码（`error.code`）：`PAYOUT_NOT_ENABLED`、`PAYER_EXISTS`、`PAYER_REQUIRED`、`FILE_EXPIRED`、`VALIDATION`、`AMOUNT_OUT_OF_RANGE`、`QUOTE_EXPIRED`、`QUOTE_NOT_READY`、`TRADE_PASSWORD_NOT_SET`、`TRADE_PASSWORD_INVALID`（带 `remaining_attempts`）、`TRADE_PASSWORD_LOCKED`、`ORDER_NOT_CANCELABLE`、`UPSTREAM`（带 UPay `code/msg`，便于排障）。

### 7.1 字段来源说明

- **国家**：静态 ISO 3166-1 alpha-2 表（前端内置，中文名 + 代码），不调 `common/area/list`（只需国家一级）
- **区号**：静态表，随国家默认联动，可改
- **日期**：前端 `yyyy-MM-dd` → 后端转 UTC 0 点毫秒字符串（实测 #9）

### 7.2 付款人（6 步 KYC）字段映射

| 步 | UI 字段 | UPay group.fieldKey | 备注 |
|---|---|---|---|
| ① 基本信息 | 名、姓 | `base.name` | 拼 `"名 姓"`；**创建后不可改**（`isEdit=false`），提交前二次确认 |
| | 本地语言姓名 | `base.nameLocal` | 必填；默认填入 `"名 姓"`，可改 |
| | 出生日期 | `base.dateOfBirth` | ms |
| | 国籍 | `base.nationality` | |
| | 性别 | `base.gender` | `Male` / `Female` |
| ② 联系方式 | 邮箱 | `contact.email` | 默认填账号邮箱 |
| | 区号 + 手机号 | `contact.callingCode` / `contact.phoneNumber` | |
| ③ 证件信息 | 证件类型 | `document[0].type` | 下拉只放常用 4 项：passport / national_id / driving_license / residence_permit |
| | 证件号码 | `document[0].number` | |
| | 签发国 | `document[0].issuingCountry` | 默认 = 国籍 |
| | 签发日期 | `document[0].issuedDate` | |
| | 有效期 | `document[0].expiryType` + `expiryDate` | 选「长期有效」→ `indefinite`，否则 `dated` + 日期必填 |
| | —（推导） | `document[0].citizenship` = 国籍、`document[0].primary` = `yes` | 不展示 |
| ④ 证件材料 | 证件照片（1 张） | `identification.materiaImage` | 上传得 fileId |
| | —（推导） | `identification.materiaType` | 由证件类型映射：passport→passport、national_id→national_id、driving_license→drivers_license、其他→other |
| | —（推导） | `identification.fileName` / `materiaDescribe` | 原文件名 / `"<证件类型> photo"` |
| ⑤ 居住地址 | 国家、省/州、城市、地址 1、地址 2、邮编 | `residentialAddress.country/province/city/line1/line2/postalCode` | |
| | 地址证明（1 张） | `residentialAddress.proofImage` | 水电账单 / 银行对账单等 |
| | —（推导） | `residentialAddress.fileName` / `describe` | 原文件名 / `"proof of address"` |
| ⑥ 职业与资金（选填） | 职业状况 | `occupationFundSource.employmentStatus` | |
| | 主要资金来源 | `occupationFundSource.sourceOfIncome` | |
| | 职业 | `occupationFundSource.occupation` | 可选输入 |

设计稿中的「月收入区间 / 预期月均交易额 / 账户主要目的」**删除**（接口无对应字段）。完成页文案改为「资料已提交」。

### 7.3 添加收款人字段映射（单页三段）

| 段 | UI 字段 | UPay 字段 | 备注 |
|---|---|---|---|
| 顶部 | 币种 / 方式 | `default.currency=USD`、`default.transferType=swift` | 固定展示，不可选 |
| 收款人信息 | 名、姓 | `beneficiary base.name`、`default.holder` | 均为 `"名 姓"` |
| | 出生日期、国籍 | `base.dateOfBirth`、`base.nationality` | |
| | 邮箱 | `contact.email` | |
| | 区号 + 电话 | `contact.callingCode`、`contact.number` | 注意收款人用 `number`，付款人用 `phoneNumber` |
| 地址 | 国家、省/州、城市、地址 1、地址 2、邮编 | 收款人 `residentialAddress.country/state/city/line1/line2/postalCode` **和** 银行 `default.holderAddressCountry/State/City/Line1/Line2/PostalCode` | 一份地址填两处；银行侧省/州、城市、地址 1、邮编必填，故 UI 全部按必填 |
| 银行信息 | 银行所在国家 | `default.country` | |
| | 银行名称 | `default.bank` | 设计稿缺，新增 |
| | Swift 代码 | `bankClearingCode.type=swift_code` + `bankClearingCode.value` | 8–11 位字母数字（接口允许 8–15） |
| | 银行账号 | `default.accountNumber` | ≤ 34 位字母数字；本地只存后 4 位 |
| | IBAN | `default.iban` | **必填**（实测 #10）；提示文案「非 IBAN 国家请联系客服」— 待 UPay 答复后调整 |

保存流程：`AddBeneficiary` → 写 `beneficiary_no` → `AddBankAccount` → 写 `bank_account_no`。第二步失败：保留记录（`complete=false`、`last_error`），列表显示「未完成 · 重试」，重试只补第二步（需前端重新提交银行段表单，因为本地不存完整卡号 / IBAN）。

## 8. 前端

### 8.1 路由（`/payout/*`，`PayoutLayout`，不套桌面 `Layout`）

| 路由 | 页面 | 对应设计稿 |
|---|---|---|
| `/payout` | 入口分发：无 payer → `kyc`；有 → `recipients` | — |
| `/payout/kyc` | 6 步向导（顶部进度条） | 图 2–5（扩展） |
| `/payout/kyc/done` | 资料已提交 → 开始汇款 | 图 6 |
| `/payout/recipients` | 选择收款人（单选 + 继续） | 图 8 |
| `/payout/recipients/new` | 添加收款人 | 图 18（扩展） |
| `/payout/new?recipient=:id` | 金额与用途 → 获取报价 | 图 9 |
| `/payout/orders/:id/confirm` | 报价确认 + 倒计时 + 退款协议勾选 + PinSheet | 图 10、11 |
| `/payout/orders/:id` | 结果 / 详情（状态、详细信息两个 tab） | 图 12–14 |
| `/payout/orders` | 汇款记录列表 | 新增 |

入口：`Layout` 导航在 `me.payout_enabled` 时显示「汇款」。

### 8.2 关键交互

- **KYC 数据只存在 React state**（向导父组件持有），不写 localStorage；刷新即丢失，页面离开前 `beforeunload` 提示
- **图片上传**：选图即上传；> 4MB 用 canvas 压缩为 JPEG（质量 0.85，逐步降到 0.6），仍超限则提示；显示缩略图 + 重新上传
- **确认页**：进入即轮询 `/quote`（2s，状态非 `awaiting_confirm` 时继续轮询，最长 60s 后提示「报价超时，取消重试」）；`Countdown` 以服务端 `expires_in` 为准；到 0 → 按钮切换为「重新获取报价」→ `/requote`；`kyc_url` 非空 → 提示去完成 KYC
- **PinSheet**：6 格 + 数字键盘；模式 `verify | setup(两次输入)`；错误显示剩余次数；锁定显示倒计时
- **结果页**：非终态每 5s 刷新详情；时间线三节点 + 失败 / 取消 / 退款特殊态
- **再转一笔 / 重复此次转出**：跳 `/payout/new?recipient=:id&amount=&usage=`

### 8.3 金额与用途页

- 扣款资产：固定显示 USDT（来自 `bootstrap.debit_symbol`），不可选
- 汇出法币金额：`≥ 5`，≤ 2 位小数；币种后缀取收款人账户币种
- 汇款用途：**必填**，≤ 140；提供快捷选项（Capital transfer / Family support / Salary / Tuition / Other）+ 自由输入
- 备注：选填，≤ 140（设计稿的 0/100 改为 0/140）

## 9. 部署

- `deploy/.env.example` 补 `UPA_*`、`PAYOUT_*` 占位
- 回调地址 `https://chl.kaitchup.fun/api/webhooks/upay-payout` 提交 UPay 验证；nginx 已整体反代 `/api/`，无需改动
- 上线后在 UPay 后台配置 IP 白名单 = VPS 出口 IP
- 运维自检：`GET /api/healthz` 附带 `payout: {enabled, debit_coin_id}`；启动时 `MerchantAsset` 打印 USDT 余额

## 10. 测试策略

| 层 | 用例 |
|---|---|
| `upayopen` 单测 | 签名：文档示例固定向量（`POST\|/api/v1/customer/add\|1774573955994\|366dde…` → `pRbX0ikM/4j/E3tBxNOO/m3Hra+4d2/r+xtRTJ6N5pk=`）；参数排序 / 嵌套对象 / 空参数以 `\|` 结尾；JWE 加解密往返（测试内生成 RSA 对）；`code≠0` 转 `APIError`；`httptest` 模拟 UPay 返回加密 `data.payload` |
| formData | 普通组 / 多组 `{"0":[…]}` 序列化与存档样例一致 |
| repo / service | `ApplyOrderStatus` 幂等（重复状态不重复插 event）；归属校验（别人的 recipient / order → 404） |
| handler | 交易密码错 5 次锁定、锁定期内拒绝、成功清零；金额边界 4.99 / 5 / 100000.01；过期报价确认 → `QUOTE_EXPIRED` |
| webhook | 构造密文 + 签名 → 回 `SUCCESS`、落库、异步同步；重复通知只处理一次；验签失败 401 但 raw log 仍在 |
| 联调（手工） | 测试环境跑完整链路：KYC → 收款人 → 20 USD 下单 → 确认（**首次真实扣测试余额**）→ 观察回调与状态到 7 |

## 11. 里程碑

| 阶段 | 内容 | 预估 |
|---|---|---|
| M1 | `upayopen`（签名 / JWE / 上传 / 订单与主体接口）+ 单测 | 1.5 d |
| M2 | 迁移、repo、中间件、交易密码、付款人 / 收款人接口 | 2 d |
| M3 | 订单接口（创建 / 报价 / 确认 / 取消 / 重报价）+ 状态服务 + 轮询 + 回调 | 2 d |
| M4 | 前端：Layout、KYC 6 步、收款人两页 | 2 d |
| M5 | 前端：金额页、确认页（倒计时 + PinSheet）、结果 / 详情、列表 | 2 d |
| M6 | 测试环境真实确认一笔 + 回调口径固化 + 文档回填 | 1 d |
| | **合计** | **≈ 10.5 人日** |

## 12. 风险与待 UPay 确认

> 逐条确认记录（含请求 / 响应原文）：[UPAY-汇款接口问题清单.md](UPAY-汇款接口问题清单.md)

| # | 问题 | 当前处理 |
|---|---|---|
| 1 | ~~KYC 材料上传 `fileType=10` 报 `10124`~~ | ✅ UPay 已修复，传 10 |
| 2 | IBAN 对非 IBAN 国家（如美国）也必填 | UI 必填；请 UPay 确认是否应按国家可选 |
| 3 | `bank/account/dynamic/form` 的币种字段 `optionKey=bank.bankAccount.default.currency`，但响应 `options` 里没有这个 key | 固定传 `USD`（实测可建）；请 UPay 补齐选项 |
| 4 | **重报价接口**（报价过期后如何重新报价、何时出现状态 12）；过期报价直接确认会怎样 | 接口到位前「取消 + 重新下单」兜底；后端确认前再校验有效期 |
| 4a | 日期字段 `format=yyyy-MM-dd` 与实际校验不一致 | ✅ 已实测只接受 ms，按 ms 实现；`format` 含义待 UPay 说明 |
| 5 | ~~回调签名 `body` 口径、是否用同一 SecretKey~~ | ✅ 源码确认（`upay-broker-server` `WebhookConsumer.post:241`）：签密文原串，key = `agent_setting.secret_key`（= `UPA_SECRET_KEY`，与 open-api 验签同源） |
| 6 | `quoteTime` 快 8 小时、`orderStatus=0` 未文档化、时间为 ms 字符串 | 已按实测处理；反馈 UPay 修正文档 |
| 7 | 新建主体 `dataCompleteStatus=2` 的缺失项是什么 | 不阻塞下单；联调时查 `informationToImprove` |
| 8 | 退款协议文本 | 先放占位链接，拿到后替换 |
| 8a | ~~测试环境确认后能否推进到 7~~ | ✅ Kaitchup 确认可推进到 7 |
| 9 | 私钥已在聊天中暴露 | 测试环境可继续用；上生产前重新生成密钥对 |

## 13. 施工清单

### 后端
- [ ] `config`：`UPA_*`、`PAYOUT_*`，`PayoutEnabled()`
- [ ] `upayopen/crypto.go`：签名 + JWE + key 解析，固定向量单测
- [ ] `upayopen/client.go` + `payout.go`：通用 `call`、上传、一期接口
- [ ] `upayopen/formdata.go` + 单测
- [ ] `upayopen/webhook.go`：解密 + 双口径验签
- [ ] 迁移 `0013_payout`
- [ ] `repo/payout.go`
- [ ] `middleware/payout.go`
- [ ] `service/payout.go`：`ApplyOrderStatus`、`SyncOrder`、下单 / 确认 / 取消 / 重报价
- [ ] `handler/payout.go`：bootstrap、options、files、payer、recipients、orders、trade-password
- [ ] `handler/payout_webhook.go` + 路由 `/api/webhooks/upay-payout`
- [ ] `job/payout_sync.go`，`main.go` 注册
- [ ] `/api/me` 增加 `payout_enabled`；healthz 增加 payout 自检
- [ ] `deploy/.env.example` 补变量

### 前端
- [ ] `api/payout.ts`
- [ ] `PayoutLayout`、`Stepper`、`CountryPicker`（含区号）、`ImageUpload`（压缩）、`Countdown`、`PinSheet`
- [ ] KYC 6 步 + 完成页
- [ ] 选择收款人、添加收款人（含未完成重试）
- [ ] 金额与用途页
- [ ] 确认页（报价轮询、倒计时、重报价、退款协议、PinSheet setup/verify）
- [ ] 结果 / 详情页（时间线、tab、再转一笔）
- [ ] 汇款记录列表；导航入口

### 联调 / 上线
- [x] 回调地址提交 UPay 验证（2026-09-25 通过；最小版端点已上线，线上冒烟：正确签名 → SUCCESS，错误签名 → 401）
- [ ] 测试环境真实确认一笔 20 USD，观察回调 → 固化验签口径（改 §6.4）
- [ ] UPay 后台配置 IP 白名单
- [ ] `UPDATE users SET payout_enabled = true WHERE email = …`
- [ ] 回填 §12 各项答复

# 汇款（Payout）功能 — 可行性分析 & 物料清单

> 版本 v0.2 · 2026-09-25 · 状态：决策已对齐，待物料到位后开工
> 变更：v0.2 按接口文档 v2 补充回调 `PO_ORDER_STATUS_CHANGE`（§4.3），状态同步改为「回调为主 + 轮询兜底」
> 技术设计：[TECH-DESIGN-汇款功能.md](TECH-DESIGN-汇款功能.md)（含 2026-09-25 测试环境实测结论；签名口径、日期格式、IBAN、fileType、报价时效等待确认项已在其 §0 解决或更新，以技术设计为准）
> 输入：Figma C 端设计稿（截图见 [assets/payout/](assets/payout/)）、《Upay OpenAPI Payout 接口文档》v2（22 个接口 + Webhook）、UPay 开发者文档 <https://upay-api.readme.io>（签名 / 加密 / 文件上传 / Webhook 规范）

## 1. 结论

**可行。** 22 个 Payout 接口 + 文件上传接口能覆盖设计稿的主流程（KYC → 选收款人 → 填金额 → 报价确认 → 结果/详情）。chl-demo 现有骨架（Go/Echo/pgx、迁移器、JWT、轮询任务、Webhook 落库、Docker 部署）可直接复用。

但**不是在现有 UPay 客户端上加几个方法**——Payout 属于另一套 OpenAPI：

| | 现有收单（`internal/upay`） | Payout（新） |
|---|---|---|
| 地址 | `/v1/payment/request`，需 NetBird | `https://openapi.upay-test.best`，**公网可达**（已实测 TLS 正常） |
| 鉴权 | `Authorization: Bearer sk_...` | `X-UPA-APIKEY/REQUESTID/TIMESTAMP/SIGN`，HMAC-SHA256 + Base64，60 秒有效 |
| 报文 | 明文 JSON | 大部分接口 **JWE 加密**（请求 `{"payload":…}`，响应 `data.payload`） |
| 成功判断 | HTTP 2xx | **`code == 0`**（实测 `code=403` 时 `success` 仍为 `true`，不能只看 `success`） |
| 回调 | HMAC，`whsec_` | 事件 `PO_ORDER_STATUS_CHANGE`；密文 `text/plain`，`event\|timestamp\|body` HMAC，须 5 秒内回 `SUCCESS` |

**主要阻塞项**（详见 §6）：UPay 侧凭据与商户后台配置、Swift 收款账户 / KYC 的真实动态表单字段。（回调事件定义已由接口文档 v2 补齐，见 §4.3）

## 2. 已对齐的决策

| # | 决策 |
|---|---|
| 定位 | **Demo / 对接样例**，仅测试环境；不做合规（牌照/AML/制裁筛查） |
| 资金 | 从**商户（代理商）在 UPay 的 USDT 余额**扣款，用户不付款；扣款币种固定 `PAYOUT_DEBIT_COIN=USDT` |
| 访问控制 | `users.payout_enabled` 白名单（SQL 手动开启）；非白名单用户看不到入口，`/api/payout/*` 返回 403。注：注册接口当前已注释关闭 |
| 付款人 | **一个 C 端用户 = 一个 payer**（本地 `user_id ↔ payerNo`），KYC 流程即 `payer/add`，含证件正反面上传 |
| 数据归属 | UPay 侧所有主体都挂在商户名下，**本地归属表**隔离；任何带 `xxxNo` 的请求先校验归属再转发（防越权） |
| 敏感数据 | 本地**只存编号 + 展示摘要**（姓名、脱敏卡号 `**** 8888`、Swift、币种）；详情实时调 UPay `info` |
| 范围 | 付款人/收款人**仅 person**；汇款方式**仅 Swift**；enterprise / Local 放二期 |
| 收款人 | UI 上 1 收款人 = 1 银行账户；保存时依次 `beneficiary/add` → `bank/account/add`，第二步失败可重试 |
| 表单 | **先写死**（按设计稿字段），缺什么补什么；不做通用动态表单渲染器 |
| 报价 | 前端轮询 `quote/info` + `validUntil` 倒计时，**用户手动确认**；过期 / 状态 12 → 重新拉报价再确认 |
| 交易密码 | 本地 6 位交易密码（bcrypt），首次汇款前设置；连续错 5 次锁 30 分钟；确认汇款前后端校验 |
| 用途/附言 | 汇款用途**改为必填**（≤140）→ `remitUsage`；备注 → `remitNote`，为空时后端用用途补齐；不传 `remitRemark` |
| 状态同步 | **回调为主** `POST /api/webhooks/upay-payout`（`PO_ORDER_STATUS_CHANGE`，见 §4.3）+ **后台轮询兜底**（回调重试 5 次后放弃，不能只靠回调） |
| 时间线 | 本地 `payout_order_events` 记录每次状态变化（回调来源取推送 `timestamp`，轮询来源取观察时刻） |
| 前端 | **移动端优先**，路由 `/payout/*`，`max-w-md` 居中；不做首页（图 1），导航加「汇款」入口 |
| 列表 | 截图 7/15/16/17 不在范围；补一个简单的汇款记录 `/payout/orders`；失败/退款在详情页展示 |

## 3. UI ↔ 接口对照

| 图 | 页面 | 调用接口 | 缺口 / 处理 |
|---|---|---|---|
| 1 | 首页 | — | **不做**。余额可用 `merchant/asset` 查，但本期不展示 |
| 2 | 进行认证（国家、名、姓、证件类型、证件号） | `common/area/list`（国家）| 证件类型枚举、fieldKey 需真实 `payer/dynamic/form` |
| 3 | 证件上传（正/反） | `common/file/upload`（`fileType=1/2`，multipart，参数不参与签名）→ `fileId` 写入 payer formData | **UI 写「小于 8M」，接口上限 4MB** → 文案改 4MB，前端超限压缩 |
| 4 | 收入情况（职业、资金来源、月收入） | 同上，写入 formData | 枚举值需真实表单 `options` |
| 5 | 预期交易（月交易额、账户目的）→ 提交 | `payer/add` → `payerNo` | 字段映射待真实表单确认 |
| 6 | 认证成功 → 开始汇款 | — | `payer/add` 成功 ≠ KYC 通过；文案改「资料已提交」。若报价状态 `7=待完成KYC`，提示审核中 |
| 8 | 选择收款人 | 本地 `payout_bank_accounts` 列表（名称 / Swift / 卡号后四位） | 不调 `beneficiary/list`（会返回商户下所有人） |
| 18 | 添加收款人（USD / Swift / 名 / 姓 / 邮箱 / 银行国家 / Swift / 账号） | `beneficiary/add`(person) → `bank/account/add`(bankAccount) | 文档示例的 bankAccount 字段是 `holderName/bankNumber/bankName/currency`，**无 swift/country** → 需真实表单确认 |
| 9 | 汇款金额与用途（扣款资产 USDT 固定、法币金额、用途、备注） | 「获取报价」= `order/creation`（`remitMethod=swift`，`thirdOrderNo` 本地生成） | 汇出法币币种由银行账户 `currency` 决定；用途改必填 |
| 10 | 确认并转出（有效期倒计时、费用明细、退款协议勾选） | 轮询 `quote/info` 至 `status=2`；取 `quoteList[status=3]` | 费用映射见 §3.1；**退款协议文本**需 UPay 提供 |
| 11 | 交易密码 | 本地校验 → `order/confirm`（`quoteNo`） | 接口无此概念，本地实现 |
| 10 | 取消订单 | `order/cancel` | 状态 6 之后不可取消（错误码 10207） |
| 12 | 结果页（时间线、再转一笔） | `order/info` + 本地 events | 接口无分状态时间戳 → 本地记录 |
| 13/14 | 详情（状态 / 详细信息 tab、收款人账户详情、重复此次转出） | `order/info` + `beneficiary/info` + `bank/account/info` | 图 14 有打码字段（下单方式/汇款…/资金…冻结），需确认来源；UPB 订单号 = `orderNo` |

### 3.1 报价字段映射（图 10）

| UI | 接口字段 |
|---|---|
| 有效期 03:00 | `validUntil - now` |
| 汇出金额 / 到账 | `destinationAmount` + `destinationCurrency` |
| 固定 / 交易 / 汇兑手续费 | `fixedFee` / `transactionFee` / `exchangeFee`（`feeCurrency`） |
| 付款总额 | `debitAmount`（文档定义为「付款总额」） |
| 支付金额 | `debitAmount − 三项手续费`（**需与 UPay 确认口径**，订单详情另有 `debitAmount` / `debitTotalAmount` 两个字段） |

所有金额按字符串 / decimal 处理，禁用 float（沿用项目约定）。

### 3.2 订单状态 → UI 时间线

| UPay `status` | 含义 | UI |
|---|---|---|
| 1, 2, 12 | 报价中 / 待确认 / 待确认新报价 | 停留在确认页（12 → 重新确认） |
| 4 | 报价失败 | 确认页报错，允许重新下单 |
| 3, 5 | 报价已确认 / 审核中 | ① 支付已收到 |
| 6 | 汇款处理中 | ② 银行处理中 |
| 7 | 汇款成功 | ③ 转账已转出 |
| 8, 10, 11 | 失败 / 退款中 / 已退款 | 详情页失败态（红色节点 + 状态文案） |
| 9 | 已取消 | 详情页取消态 |

终态：4、7、8、9、11。轮询任务只扫非终态订单。

## 4. 技术方案概要

### 4.1 后端

- **新包 `internal/upayopen`**（不改动现有 `internal/upay`）
  - 签名：`METHOD|URI|TIMESTAMP|REQUEST_ID|SIGN_PARAM`，参数按 key 升序 `k=v&…`（嵌套对象值为 JSON），HMAC-SHA256(SecretKey) → Base64
  - 加密：JWE compact，`alg=RSA-OAEP-256`、`enc=A128GCM`、`zip=DEF`、`kid=sha256(平台公钥PEM)`；响应用商户私钥解密。候选库 `github.com/go-jose/go-jose/v4`（需验证 DEF 解压上限对大列表响应是否够用）
  - 响应判断 `code == 0`；`X-UPA-REQUESTID` 用 UUID 并落日志便于对账
  - `amount` 用 `json.Number` 保证签名串与报文数值一致
- **新配置**（仅环境变量）：`UPAY_OPENAPI_BASE_URL`、`UPAY_OPENAPI_KEY`、`UPAY_OPENAPI_SECRET`、`UPAY_OPENAPI_PLATFORM_PUBKEY`、`UPAY_OPENAPI_MERCHANT_PRIVKEY`（文件路径）、`UPAY_OPENAPI_WEBHOOK_SECRET`、`PAYOUT_DEBIT_COIN=USDT`
- **迁移**：
  - `users.payout_enabled BOOLEAN DEFAULT false`、`users.trade_password_hash`、`trade_password_fail_count`、`trade_password_locked_until`
  - `payout_payers(user_id UNIQUE, payer_no, name, status)`
  - `payout_beneficiaries(user_id, beneficiary_no, name, status)`
  - `payout_bank_accounts(user_id, beneficiary_no, bank_account_no NULL, swift, bank_number_masked, currency, country, status)` — `bank_account_no` 为空即「未完成」可重试
  - `payout_orders(id, user_id, third_order_no UNIQUE, order_no, bank_account_no, amount, currency, usage, note, status, quote_no, debit_total, raw_last_info JSONB, …)`
  - `payout_order_events(order_id, status, observed_at, source[poll|webhook|api])`
- **接口**（JWT + `payout_enabled` 中间件）：`/api/payout/payer`（GET/POST，含上传代理）、`/api/payout/beneficiaries`（GET/POST）、`/api/payout/orders`（POST 创建、GET 列表、GET `:id`、GET `:id/quote`、POST `:id/confirm`、POST `:id/cancel`）、`/api/payout/trade-password`（POST 设置）
- **轮询任务** `job/payout_reconcile.go`：参照 `job/reconcile.go`，定时扫非终态订单调 `order/info`，状态变化写 events
- **回调** `POST /api/webhooks/upay-payout`：验签 → 解密 → 幂等 → 更新状态，细节见 §4.3

### 4.2 前端

- `/payout/*` 路由，独立 `PayoutLayout`（`max-w-md` 移动端容器，不套现有桌面 Layout）
- 页面：`kyc`（4 步向导）、`kyc/done`、`beneficiaries`、`beneficiaries/new`、`new`（金额用途）、`orders/:id/confirm`（报价 + 倒计时 + 交易密码弹层）、`orders/:id`（结果/详情 tab）、`orders`（列表）
- 入口：`/api/me` 返回 `payout_enabled`，导航条件渲染「汇款」；点入时无 payer → KYC，有 payer → 选收款人

### 4.3 回调处理（`PO_ORDER_STATUS_CHANGE`）

来源：接口文档 v2「Webhook 说明」+ 文档站通用推送规范（<https://upay-api.readme.io/reference/请求说明>）。

**报文**：`POST`，`Content-Type: text/plain`，Header `X-UPA-REQUESTID` / `X-UPA-TIMESTAMP` / `X-UPA-SIGN`，body 为用**商户公钥**加密的 JWE 串。解密后：

```json
{
  "event": "PO_ORDER_STATUS_CHANGE",
  "trace": "TRACE202609250001",
  "orderNo": "WH202609250001",
  "timestamp": 1727229600000,
  "detail": "{\"payerNo\":\"…\",\"bankAccountNo\":\"…\",\"orderNo\":\"ORD202609240001\",\"status\":7}"
}
```

⚠️ **两个 `orderNo` 含义不同**：外层 `orderNo` 是**本次通知的唯一号**（用作幂等键）；`detail.orderNo` 才是**汇款业务订单号**（对应 `payout_orders.order_no`）。`detail` 是 JSON **字符串**，需二次解析。

**处理流程**（沿用现有 `ProdWebhook`「先验签 + 去重」的套路）：

1. 读原始 body，先整体写入 `webhook_raw_log`（source=`upay-busi-payout`），便于排障
2. 用商户私钥解密 → 取 `event`
3. 验签：`Base64(HMAC-SHA256(SecretKey, event + "|" + X-UPA-TIMESTAMP + "|" + body))`，失败返回 401；时间戳超出 ±5 分钟拒绝（防重放）
4. 按外层 `orderNo` 去重（唯一索引），重复直接回 `SUCCESS`
5. 解析 `detail` → 用 `detail.orderNo` 找本地订单（找不到只记日志，回 `SUCCESS`，避免无限重试）
6. **不直接信 `detail.status`**：再调一次 `order/info` 取权威状态写库（UPay 状态并非单调，重试可能乱序到达），同时写 `payout_order_events(source=webhook, observed_at=timestamp)`
7. 5 秒内回纯文本 `SUCCESS`；第 6 步若慢，改为入库后异步处理（UPay 失败每 3 秒重试、最多 5 次）

**已确认**（`upay-broker-server` `WebhookConsumer.post` 源码）：签名串里的 `body` 是**密文原串**，key 与 API 签名同为 SecretKey。实现细节以 [TECH-DESIGN-汇款功能.md](TECH-DESIGN-汇款功能.md) §6.4 为准。

## 5. 风险 & 待验证点

| 风险 | 影响 | 应对 |
|---|---|---|
| 加密接口的签名参数是**明文参数**还是 `payload=<jwe>`，文档未写明 | 所有加密接口签名失败（1005） | 联调第一步用 `beneficiary/list` 两种方式各试一次，结论写回本文档 |
| 回调签名口径（body 是密文还是明文）未写明 | 回调验签失败 → 状态只能靠轮询 | 验签失败时仍先落 `webhook_raw_log`；首条真实推送两种口径各算一次定下来；轮询兜底保证状态最终一致 |
| 回调地址需 UPay 验证后才会推送 | 联调初期收不到回调 | 尽早提交回调地址；未通过前轮询照常工作 |
| Swift 银行账户、KYC 真实字段与文档示例不符 | 写死的表单要返工 | 拿到凭据后先调 3 个 `dynamic/form` 接口，**把真实响应存进 `docs/assets/payout/forms/`** 再开始写表单 |
| 测试环境是否真实出款 / 状态是否会自动流转到 7 | 无法演示完整时间线 | 向 UPay 确认测试环境行为，是否有模拟推进状态的手段 |
| 商户测试余额不足 | 报价/确认失败（10082） | 向 UPay 申请测试 USDT；列表页展示错误码 |
| 用任何白名单用户都在消耗商户余额 | 测试资金被耗尽 | 白名单 + 交易密码；可后续加单笔/日限额 |
| JWE `zip=DEF` 解压上限 | 大列表响应解密失败 | 列表 `pageSize` 控制在 ≤ 20，必要时换库 |

## 6. 物料清单

### 6.1 需向 UPay 索取 / 确认（阻塞项加粗）

| # | 物料 | 用途 |
|---|---|---|
| 1 | **测试环境 商户后台账号** | 获取密钥、上传商户公钥、配 IP 白名单、配回调地址 |
| 2 | **API Key + Secret Key + 平台公钥** | 签名、请求加密 |
| 3 | **代理商开通 Payout 权限 + 测试 USDT 余额** | 否则 `debit/coin/list` 为空 / 余额不足 |
| 4 | **回调地址登记 + 验证通过**；确认回调签名用的 key 与 `body` 口径（密文 / 明文） | 回调验签（事件定义已在接口文档 v2 中给出 ✅） |
| 5 | **加密接口签名口径**（签明文参数还是 payload） | 见 §5 |
| 6 | Swift 汇款支持的**国家 / 币种范围**，bankAccount 表单中 swift / 国家字段 | 添加收款人页（图 18） |
| 7 | payer 表单中**证件照 fileId 对应的 fieldKey**、证件类型 / 职业 / 资金来源 / 收入 / 交易目的的枚举 | KYC 页（图 2–5） |
| 8 | `remitUsage` 是否有标准枚举（图 14 显示 `Capital transfer`）；`remitNote` 能否为空 | 金额用途页（图 9） |
| 9 | 「支付金额」与「付款总额」的口径（`debitAmount` vs `debitTotalAmount`） | 确认页（图 10） |
| 10 | **退款协议文本 / 链接** | 确认页勾选项 |
| 11 | 测试环境是否会真实推进订单状态，有无模拟手段 | 演示结果页时间线 |
| 12 | 图 14 打码字段（下单方式、汇款…、资金…冻结）的含义和来源 | 详情页 |

### 6.2 你自己准备（已有 VPS、域名）

| # | 物料 | 状态 / 说明 |
|---|---|---|
| 1 | VPS 固定出口 IP `185.92.181.220` | ✅ 已有；提交到 UPay 后台 IP 白名单（不设则默认全放行） |
| 2 | 域名 + HTTPS | ✅ 已有 `chl.kaitchup.fun`；回调地址 `https://chl.kaitchup.fun/api/webhooks/upay-payout`，**需提交 UPay 验证** |
| 3 | 商户 RSA-2048 密钥对 | ⬜ 在 VPS 上 `openssl genrsa -out private.pem 2048 && openssl rsa -in private.pem -pubout -out public.pem`；公钥上传后台，私钥仅挂载进容器，不入仓 |
| 4 | 白名单用户 | ⬜ `UPDATE users SET payout_enabled = true WHERE email = '…'` |
| 5 | 测试 KYC 证件图 | ⬜ 准备 JPG/PNG ≤ 4MB 的样例证件正反面（测试用） |
| 6 | 测试收款银行账户 | ⬜ 可用 UPay 提供的测试账户，或确认测试环境是否校验真实性 |

### 6.3 开发产出

| # | 产出 | 预估 |
|---|---|---|
| 1 | `internal/upayopen`：签名 + JWE + 22 接口 + 文件上传 + 单测（用文档示例签名做固定向量） | 2 d |
| 2 | 迁移 + repo + 归属校验 + 白名单中间件 + 交易密码 | 1.5 d |
| 3 | Payout handler（payer / beneficiary / order / quote / confirm / cancel） | 2 d |
| 4 | 回调端点（解密 + 验签 + 去重 + 回查）+ 轮询任务 + events | 1.5 d |
| 5 | 前端 8 个移动端页面 + 倒计时 + 交易密码键盘 | 3–4 d |
| 6 | 联调 + 文档回填（签名口径、真实表单、状态流转） | 2 d |
| | **合计** | **≈ 12 人日**（物料齐备后） |

## 7. 分期

- **P0 联调探路**（物料 6.1 #1–3 到位即可开始）：写 `upayopen` 客户端，调通 `debit/coin/list`（不加密）→ `beneficiary/list`（加密，定签名口径）→ 拉三个 `dynamic/form` 存档；同时提交回调地址给 UPay 验证。**这一步决定后续表单能否按设计稿写死。**
- **P1 主流程**：KYC（含上传）→ 添加收款人 → 下单 → 报价确认 → 交易密码 → 结果页；回调 + 轮询同步状态。
- **P2 完善**：订单列表、失败/退款展示、再转一笔 / 重复此次转出。
- **二期（未排期）**：enterprise 主体、Local 汇款、先收款后汇款（真实资金来源）、单笔/日限额。

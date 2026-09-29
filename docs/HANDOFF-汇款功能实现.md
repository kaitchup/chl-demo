# 汇款功能实现 — 交接记录（STAR）

> 2026-09-25 起草，2026-09-27 更新 · 实现已由 Kaitchup 提交并推送到 `main`（`d3e51e5`，原 `feature/payout` 已删）；后续修改在 `feature/payout-followup`（未提交）· 2026-09-28 本地 API 冒烟已跑通（确认汇款除外），**前端未点测；UPay 报价缺扣款总额 / 手续费（问题清单 #6），修复前不能上线**
> 需求：[PRD-汇款功能-可行性分析.md](PRD-汇款功能-可行性分析.md) · 设计：[TECH-DESIGN-汇款功能.md](TECH-DESIGN-汇款功能.md) · UPay 问题：[UPAY-汇款接口问题清单.md](UPAY-汇款接口问题清单.md)

## S — 背景

- chl-demo 已上线**最小版回调端点** `POST /api/webhooks/upay-payout`（已在 `main`，已部署，UPay 已验证通过，source=`upay-busi-payout`）。
- UPay 测试环境接口已全部实测（签名 / JWE / 表单 / 下单报价 / 上传 fileType=10 已修复），结论在 TECH-DESIGN §0。
- Kaitchup 决定：**不做真实确认一笔**（已知测试环境可推进到 7），直接按技术方案实现完整汇款功能。

## T — 任务

按 TECH-DESIGN §2–§8 实现：后端（迁移、UPay OpenAPI 客户端、repo、service、handler、中间件、轮询任务、完整回调）+ 前端移动端 `/payout/*` 全部页面，并在本地跑通后交给 Kaitchup 部署。

## A — 已做的事

### 后端（`backend/`）

| 文件 | 内容 |
|---|---|
| `db/migrations/0013_payout.{up,down}.sql` | `users` 加 `payout_enabled` + 交易密码 3 列；新表 `payout_payers` / `payout_recipients` / `payout_orders` / `payout_order_events` / `payout_webhook_events` |
| `internal/upayopen/client.go` | 通用客户端：签明文参数、JWE（RSA-OAEP-256+A128GCM+DEF）、`code==0` 判定、`APIError`、multipart 上传（签名串参数为空、分段设 Content-Type） |
| `internal/upayopen/payout.go` | DebitCoins、Countries(area/list)、AddPayer / AddBeneficiary / AddBankAccount、Beneficiary/BankAccountInfo、CreateOrder（`json.Number` 保金额一致）、QuoteInfo、ConfirmOrder、CancelOrder、OrderInfo；`Str` 兼容字符串/数字；状态常量 |
| `internal/upayopen/formdata.go` | formData 构造器（单组 / 多组 `{"0":[…]}`，空值自动省略）+ `Info.Flatten()` |
| `internal/repo/payout.go` | 白名单、交易密码（失败计数 / 锁定）、payer、recipient、order、events（`UNIQUE(order_id,status)` 幂等）、回调去重 |
| `internal/service/payout.go` | 全部业务规则：校验、PayerForm / BankForm 字段映射（§7.2 / §7.3）、建收款人两步 + 失败重试、下单（5~100000、2 位小数、note 为空用 usage）、报价（`pay_amount = debitAmount − 三项费`，`expires_in` 按 `validUntil`）、确认（仅本地状态 2/12 可确认、提前 5s 视为过期、交易密码、确认后 2s 异步回查）、取消、重报价、SyncOrder |
| `internal/handler/payout.go` | `/api/payout/*` 16 个接口（见 main.go 路由），错误体 `{code,message,details}` |
| `internal/handler/payout_webhook.go` | 升级为完整版：验签后按外层 `orderNo` 去重 → 回 `SUCCESS` → goroutine `SyncByOrderNo` → 标记已处理 |
| `internal/middleware/payout.go` | `payout_enabled` 白名单（403 `PAYOUT_NOT_ENABLED`） |
| `internal/job/payout_sync.go` | 每 `PAYOUT_SYNC_INTERVAL` 扫 3/5/6/10 且 7 天内的订单；补处理 1 分钟未完成的回调 |
| `internal/config/config.go` | 新增 `UPA_HOST/UPA_API_KEY/UPA_PLATFORM_PUBLIC_KEY`、`PAYOUT_DEBIT_SYMBOL`(USDT)、`PAYOUT_UPLOAD_FILE_TYPE`(10)、`PAYOUT_SYNC_INTERVAL`(60s)；`PayoutEnabled()` 需 5 个 `UPA_*` 齐全 |
| `cmd/server/main.go` | 装配 service / job / 路由；启动时预热国家列表（area/list 实测要 ~9s） |
| `internal/handler/account.go` | `/api/me` 返回 `payout_enabled` |

### 前端（`frontend/src/`）

| 文件 | 内容 |
|---|---|
| `api/payout.ts` | 类型、`payoutApi`、`PayoutApiError`、中文标签、`errorText()` |
| `components/payout/ui.tsx` | `PayoutLayout`（max-w-md + 进度条 + 底部按钮）、表单控件、`CountrySelect`、`ImageUpload`（>4MB canvas 压缩）、`useCountdown`、`PinSheet`（verify / setup 两次输入）、`BankAvatar` |
| `pages/payout/index.tsx` | 路由：`/payout`、`kyc`、`kyc/done`、`recipients`、`recipients/new`（`?retry=id` 只补银行段）、`new`、`orders`、`orders/:id`、`orders/:id/confirm` |
| `pages/payout/Kyc.tsx` | 6 步向导（数据只在内存；服务端 VALIDATION 按字段跳回对应步；FILE_EXPIRED 清图回第 4 步） |
| `pages/payout/{Entry,Recipients,Amount,Confirm,Orders}.tsx` | 入口分发 / 资料已提交、选择与添加收款人、金额与用途、确认并转出（2s 轮询报价、倒计时、过期→重新获取报价、退款协议勾选、交易密码）、详情（状态时间线 + 详细信息 tab）、记录列表 |
| `App.tsx` / `components/Layout.tsx` / `api/client.ts` | 挂 `/payout/*`（不套桌面 Layout）；导航按 `me.payout_enabled` 显示「汇款」 |

### 与技术方案的差异（已按此实现，TECH-DESIGN 尚未全部回写）

1. **重报价 = 取消旧单 + 同参数新建**（`POST /orders/:id/requote`），不用「过期报价调 confirm 被动触发」—— 后者在双方时钟不一致时可能直接扣款（问题清单 #2）。
2. 交易密码错误返回 **422**（不是 401）：前端 axios 拦截器遇 401 会登出。
3. 未实现 `GET /api/payout/recipients/:id`（详情页用本地摘要足够）。
4. 电话区号：前端自由输入 `+数字` 并校验，没有下拉（OpenAPI 无区号来源，问题清单 #4）。
5. 上传大小上限 10MB（fileType=10 的限制），前端图片超 4MB 自动压缩。

## R — 当前结果

### 已验证

- `go build ./...`、`go vet ./...`、`go test ./...` 全部通过。新增单测：
  - `upayopen`：文档签名向量、回调往返（篡改 / 错密钥 / 重推窗口 / 过期）、`httptest` 模拟 UPay（服务端按明文验签、解请求 JWE、回加密响应）、金额签名一致、`Str` 解析、formData 形状、`Info.Flatten`
  - `service`：PayerForm 字段映射与推导、长期有效证件去掉 expiryDate、6 类校验、BankForm、NormalizeAmount 边界、decSub（68.06−10−3−0=55.06）、Stage 映射
- **真实 UPay 集成测试（只读）通过**：`go test -tags integration ./internal/upayopen/ -run Integration`，覆盖 DebitCoins（USDT=458884）、area/list（157 国）、OrderInfo、QuoteInfo、BeneficiaryInfo+Flatten。
- 前端 `tsc --noEmit` 与 `npm run build` 通过。

### 未验证（接手后第一件事）

- **从未连真实数据库运行过**：0013 迁移、`repo/payout.go` 全部 SQL（尤其 `RecordTradePasswordFailure`、`ApplyPayoutOrderStatus` 事务、`ListUnprocessedPayoutWebhooks` 的 `make_interval`）、handler 端到端、前端页面与后端联调，都没跑过。
- 写接口（建 payer / 收款人 / 下单 / 确认 / 取消 / 重报价）未经本实现调用过（之前的实测用的是临时探测脚本）。

### 中断点 & 待 Kaitchup 决定

本地联调时我自行用 `embedded-postgres`、随后又准备用 PG17 在临时目录 `initdb` 新建实例，**被 Kaitchup 叫停**：本地已有数据库，不应另建。待确认：

1. 本地库连接信息（端口 / 库名 / 用户）及是否在运行（PG17 程序在 `D:\Program\PostgreSQL\17\pgsql`，2026-09-25 时 5432 未监听）。
2. 能否在该库上执行 0013 迁移并建测试用户、打开 `payout_enabled`；否则改为直接部署 VPS 验证。

临时实例已停止（5433 已释放）；会话临时目录里残留的 embedded-postgres 文件与探测脚本不影响仓库，可忽略。

## 2026-09-27 跟进（`feature/payout-followup`，未提交）

- 静态审查 `repo/payout.go` 全部 SQL：`ListPayoutOrders` 的游标参数加 `::bigint`（原写法 `$2` 会被推断为 int4）；其余无问题
- `healthz` 增加 `payout_enabled`
- `deploy/.env.example`：列出 5 个 `UPA_*` + 可选 `PAYOUT_*`
- TECH-DESIGN 回写上面「差异」5 条（§6.3 重报价、§7 接口表 / 错误码、§7.1 区号），§13 施工清单打勾，新增「本地连库联调」待办
- 本地库：WSL（`Ubuntu-26.04`）里的 PG17，经 Kaitchup 同意新建角色与库，`postgres://chl:chl@127.0.0.1:5432/chl`。Windows 上**必须写 `127.0.0.1`**：`localhost` 会先试 `::1`，WSL mirrored 网络不转发 IPv6 回环，每次连接卡到超时
- 后端连该库启动成功，0001–0013 迁移全部执行；新增 `repo/payout_test.go`（`TEST_DATABASE_URL` 门控）覆盖 `repo/payout.go` 全部 SQL（锁定计数、状态事件幂等、游标分页、`make_interval`、NULL JSONB 扫描），通过
- 修 `activate_test.go` 清理不生效（`defer pool.Close()` 先于 `t.Cleanup` 执行，测试数据残留）
- 本地 DSN 示例 `localhost` → `127.0.0.1`（config 默认值、README、AGENTS、activate_test 注释）
- **待 Kaitchup 在本地 `deploy/.env` 填 5 个 `UPA_*`**，之后做接手步骤 3–5（API 冒烟 + 前端联调）

## 2026-09-28 本地 API 冒烟（真实 UPay 测试环境，未确认任何汇款）

测试账号 `payout-smoke@chl.local`（user 6，口令见本地 `docs/TEST-ACCOUNTS.md`，已开 `payout_enabled`，交易密码已设）。

| 环节 | 结果 |
|---|---|
| `/me`、bootstrap、options（157 国）、白名单 403、无 token 401 | ✅ |
| 上传 fileType=10（png 两张）、非法类型 422 | ✅ |
| 建 payer（`Smoke Tester`）、重复 409、日期格式错 422 | ✅ UPay `payer/add` code=0 |
| 建收款人：两步成功 / 第 2 步失败保留记录 / 重试只补第 2 步 | ✅（DE、GB 银行可建；US 银行被 #7 拦） |
| 下单校验：金额越界、3 位小数、缺用途、收款人不存在 | ✅ 422 / 404 |
| 下单 → ~1 秒到 `2` 出报价，倒计时 180 秒 | ✅（仅 US 收款人；DE / GB 下单 `8019`，#8） |
| 报价未过期时重报价 409；过期后重报价 = 取消旧单 + 新建 | ✅ |
| 交易密码：格式 422、重复设置 409、错 4 次 422（剩余次数）、第 5 次起 423 锁 30 分钟 | ✅ UPay `order/confirm` 调用 0 次 |
| 取消 200、再取消 409、确认已取消订单 409 | ✅ |
| 订单列表 / 详情（时间线） | ✅ |

**修复**：建收款人 / 重试的响应返回的是内存对象（`created_at` 为零值、重试后 `last_error` 仍是旧错误）→ `addBank` 成功后重新读库（`service/payout.go`）。

**发现的 UPay 问题**（已写入问题清单）：
- **#6 扣款总额 / 手续费 / 到账金额字段为 `null`**（只有 `channelTotalAmount`、`paymentAmount` 有值，含义待确认）：确认页显示不了实付金额，**上线阻塞**
- #7 IBAN 现在要求国家码与银行国家一致但仍必填 → 美国银行账户无法添加
- #8 DE+USD、GB+USD 均 `8019` 未开放，且无接口查可用国家 → 测试环境目前新建不出能下单的收款人（本次借用 09-25 的 US bankAccount，本地收款人 id 5）
- #9 报价过期后 `quote/info` 短时 500（一次）

**未覆盖**：确认汇款（按约定不做）；UPay 真实回调推到本地（回调只推生产地址）；同步任务处理 3/5/6/10（需要已确认订单）；前端页面点测。

## 接手步骤

1. `git checkout feature/payout-followup`（或基于 `main` 新建），`git status` 查看未提交的跟进修改（见下方「2026-09-27 跟进」）。
2. 按 Kaitchup 给的库启动后端（例）：
   ```bash
   cd backend
   # UPA_* 五项取自 deploy/.env（不入仓）；UPA_SECRET_KEY 含 $ ^ ( !，用单引号
   DATABASE_URL="postgres://chl:chl@127.0.0.1:5432/chl?sslmode=disable" JWT_SECRET=dev-secret PORT=8090 PAYOUT_SYNC_INTERVAL=15s \
   UPA_HOST=https://openapi.upay-test.best UPA_API_KEY=… UPA_SECRET_KEY='…' \
   UPA_PLATFORM_PUBLIC_KEY=… UPA_MERCHANT_PRIVATE_KEY=… go run ./cmd/server
   ```
   启动日志应有 `payout enabled (host=… debit=USDT fileType=10)`。
3. 建测试用户（注册接口已关闭，需 SQL 插入 bcrypt 口令）并 `UPDATE users SET payout_enabled=true WHERE email=…`。
4. API 冒烟（不确认汇款）：bootstrap → options → files（fileType=10）→ payer → recipients → orders → quote → cancel / requote；交易密码：设置、错 5 次锁定、422 不登出。
5. 前端 `npm run dev`（:5173）走一遍 `/payout` 全流程（手机宽度）。
6. 修问题 → 回写 TECH-DESIGN（上面「差异」5 条 + §13 施工清单打勾）→ 提交 → Kaitchup 部署（`deploy/.env` 需补 `UPA_HOST/UPA_API_KEY/UPA_PLATFORM_PUBLIC_KEY`，现有两项已在）。

## 注意事项

- **不要真实确认汇款**（Kaitchup 明确不做）；联调停在报价 / 取消。
- **不要自行新建数据库或实例**，用 Kaitchup 指定的库。
- UPay 行为有疑问先查 `D:\Git\dochar\upay` 源码（payout 的 open-api 在 `origin/feature/260910-payout` 分支）。
- 测试环境残留：订单 `PO1790318127PROBE` 停在 12（复活 bug 证据，勿动）；可复用 payer `2103416109408985088`（fileType=10 材料）、beneficiary `2103372347110658048`、bankAccount `2103372468649005056`。
- `gofmt -l` 会报 upayopen 若干文件：仅 CRLF（`core.autocrlf=true`），不是格式问题。

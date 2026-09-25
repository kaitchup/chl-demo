# UPay 汇款（Payout）接口问题清单

> 2026-09-25 · 测试环境 `https://openapi.upay-test.best` · 商户 10103（puppy）
> 用途：与 UPay 逐条确认。每条附请求 / 响应原文，确认后在「结论」处填写，并同步到 [TECH-DESIGN-汇款功能.md](TECH-DESIGN-汇款功能.md) §0 / §12。
> 说明：API Key 已打码；签名 60 秒过期，原文可直接转给 UPay。

| # | 问题 | 影响 | 状态 |
|---|---|---|---|
| 1 | 日期字段格式：`format` 与 `description` 矛盾 | KYC / 收款人日期字段怎么传 | ✅ 已实测：只接受毫秒时间戳（`format` 字段含义仍待 UPay 说明） |
| 2 | 缺少「重新报价」接口 | 报价过期后用户无法继续，只能取消重下 | ⬜ 待确认（源码 + 实测：无独立接口，confirm 过期报价被动触发；是否为正式方式待 UPay 答复） |
| 3 | KYC 材料上传的 `fileType`：10 报格式错，37 报参数错 | 付款人证件、地址证明无法按规范上传 | ✅ 已修复（2026-09-25 13:27 复测通过，应传 10） |
| 4 | 银行账户币种选项缺失 | 添加收款人时无法给出币种列表 | 🔴 已定位：OpenAPI 分支不下发币种（及区号）选项，也无替代接口，待 UPay 修复 |
| 5 | 🐞 已取消订单可被 `confirm` 复活 | 已取消订单可能被重新确认并扣款 | 🔴 已实测复现，待 UPay 修复 |

---

## 1. 日期字段格式：`yyyy-MM-dd` 还是毫秒时间戳？

**现象**：`payer/dynamic/form`、`beneficiary/dynamic/form` 中所有 `date` 类型字段，`format` 与 `description` 描述不一致。

**请求**

```
POST https://openapi.upay-test.best/api/v1/payout/payer/dynamic/form
（无请求体，不加密）
```

**响应（节选）**

```json
{"fieldName":"Date of Birth","fieldKey":"dateOfBirth","fieldType":"date",
 "format":"yyyy-MM-dd","description":"Unix timestamp in milliseconds (ms)","isEdit":true,"required":true}
{"fieldName":"Issue Date","fieldKey":"issuedDate","fieldType":"date",
 "format":"yyyy-MM-dd","description":"Unix timestamp in milliseconds (ms)","isEdit":true,"required":true}
{"fieldName":"Expiry Date","fieldKey":"expiryDate","fieldType":"date",
 "format":"yyyy-MM-dd","description":"Unix timestamp in milliseconds (ms)","isEdit":true,"required":false}
```

**实测**：按毫秒时间戳字符串提交 `payer/add` 成功（`payerNo=2103372212922290176`），`payer/info` 原样返回毫秒值。`yyyy-MM-dd` 未测试。

`payer/add` 请求明文（加密前的 `formData`，节选日期部分）：

```json
{"code":"base","fields":[… {"fieldKey":"dateOfBirth","fieldValue":"632361600000"} …]}
{"code":"document","fields":{"0":[… {"fieldKey":"issuedDate","fieldValue":"1577836800000"},
                                  {"fieldKey":"expiryDate","fieldValue":"1893456000000"} …]}}
```

响应：`{"code":0,"msg":"Success","data":{"payerNo":"2103372212922290176"}}`

`payer/info` 返回（节选）：`{"fieldKey":"issuedDate","fieldValue":"1577836800000","fieldName":"Issue Date"}`

**补测 `yyyy-MM-dd`（2026-09-25）**：其余字段与上面成功的请求相同，只改日期值。

| 请求 | 日期取值 | 响应 |
|---|---|---|
| `payer/add` | `dateOfBirth="1990-01-15"`、`issuedDate="2020-01-01"`、`expiryDate="2030-01-01"` | `{"code":8002,"msg":"Basic Information Date of Birth has an invalid format. Only a millisecond timestamp is accepted."}` ❌ |
| `beneficiary/add` | `dateOfBirth="1988-05-20"` | `{"code":8002,"msg":"Basic Information Date of Birth has an invalid format. Only a millisecond timestamp is accepted."}` ❌ |
| `payer/add` | `dateOfBirth="632361600000"`（ms），`issuedDate="2020-01-01"`、`expiryDate="2030-01-01"` | `{"code":8002,"msg":"Document Information Issue Date has an invalid format. Only a millisecond timestamp is accepted."}` ❌ |

三次均未创建数据。

**请 UPay 确认**
- [x] ~~日期字段的标准传法是哪种？~~ → 服务端明确只接受毫秒时间戳
- [ ] 表单返回的 `format: "yyyy-MM-dd"` 是否只是给前端展示用的格式？若不是，请改成与校验一致，避免误导
- [ ] 毫秒时间戳按哪个时区的 0 点？（我方按 UTC 0 点，如 1990-01-15 → `632361600000`）

**结论**：✅ **只接受毫秒时间戳字符串**（付款人、收款人均已验证；`dateOfBirth`、`issuedDate` 均校验，`expiryDate` 同组同规则）。`format` 字段与校验不一致，待 UPay 说明或修正；实现按 ms，不受影响。

---

## 2. 报价过期后，如何重新报价？

**现象**：接口文档的 22 个接口里没有「重新报价」接口；报价过期后，订单和报价状态都没有变化。但文档里有订单状态 `12=待确认新报价`、报价状态 `6=报价已替换`，说明存在重报价机制。

**复现步骤**

1. `order/creation`（20 USD，Swift）

   请求明文：
   ```json
   {"debitCoinId":"458884","thirdOrderNo":"PO1790318127PROBE","payerNo":"2103372212922290176",
    "bankAccountNo":"2103372468649005056","amount":20,"remitMethod":"swift",
    "remitUsage":"Capital transfer","remitNote":"probe test"}
   ```
   响应：
   ```json
   {"crateAt":"1790318126800","informationToImprove":[],"materialsStatus":null,
    "orderNo":"2026092502103372790935130112","orderStatus":0,"thirdOrderNo":"PO1790318127PROBE"}
   ```

2. 5 秒后 `order/quote/info`（请求：`{"thirdOrderNo":"PO1790318127PROBE"}`），响应：
   ```json
   {"crateAt":"1790318127000","orderNo":"2026092502103372790935130112","status":2,
    "quoteList":[{"batchNo":"QB1790318126866c38460794a62","channelCurrency":"USD","channelTotalAmount":"55",
      "debitAmount":"68.06","debitCoin":"USDT","destinationAmount":"20","destinationCurrency":"USD",
      "exchangeFee":"0","feeCurrency":"USDT","fixedFee":"10","kycUrl":null,"quoteId":"128",
      "quoteNo":"Q1790318127097b1d1e66cc535","quoteTime":"1790346927000","remitMethod":"swift",
      "remitMethodName":null,"status":3,"statusName":null,"transactionFee":"3","validUntil":"1790318307000"}],
    "thirdOrderNo":"PO1790318127PROBE"}
   ```
   报价有效期 `validUntil − crateAt = 180 秒`（到 10:38:27 UTC+4）。

3. 过期后继续调 `order/quote/info`：

   | 本地时间（UTC+4） | 订单 status | 报价（quoteNo, status, validUntil） |
   |---|---|---|
   | 10:37:19（过期前） | 2 | Q1790318127097b1d1e66cc535, 3, 1790318307000 |
   | 10:38:09（过期前） | 2 | 同上 |
   | 10:39:00（**过期后 33 秒**） | 2 | 同上 —— **仍为 3=可用，无新报价** |

4. 随后 `order/cancel` 取消，状态 9。

**源码核对（2026-09-25，`upay-open-api@feature/260910-payout`、`upay-payout-server@main 87dfa61`）**

没有独立的重报价接口，重报价**只在「用过期报价调 confirm」时被动触发**：

1. open-api `PayoutOrderServiceImpl.confirmRemit` → Dubbo `PayoutOrderClient.confirmRemit`
2. payout `PayoutOrderServiceImpl.confirmRemit:391`：**只校验余额，不校验报价有效期、订单状态、报价状态**；订单置 `3=报价已确认`，发确认 MQ，同步返回 `orderStatus=3`
3. MQ 消费 `PayoutOrderOrchestrator.processConfirmedOrder:280`：报价过期 → `PayoutRequoteOrchestrator.requote`（原渠道重报价）
4. 成功 → 订单 `12`，旧报价 `EXPIRED`，`quoteBatchNo` 切换到新批次（`enterWaiting:105`）；无可用报价 → 订单 `4`（`markQuoteFailed:145`）
5. `quote/info` 按当前 `quoteBatchNo` 查询（`getOrderQuoteInfo:357`），可查到新报价 → 用新 quoteNo 再 confirm

服务端**没有任何定时任务**把状态 2 订单的过期报价置为过期，所以只查 `quote/info` 永远看不到过期（与上表实测一致）；过期只能调用方按 `validUntil` 自行判断。

实测见第 5 条（用已取消订单触发，9 → 3 → 12 得到新报价，重报价链路本身可用）。

**请 UPay 确认**
- [x] ~~重新报价的接口是什么？~~ → 无独立接口，用过期报价调 `confirm` 被动触发
- [ ] 这是否就是对外的正式重报价方式？若是，请写进文档（含「同步返回 3、随后异步变 12/4」的语义）；若不是，请提供独立的重报价接口 —— **用 confirm 触发有风险：调用方判断已过期而服务端认为未过期时，会直接确认扣款**
- [ ] 建议：状态 2 的订单报价过期时主动置为 `5=已过期`（最好推回调），让调用方能感知
- [ ] `confirm` 同步返回的 `orderStatus=3` 在报价过期时不是最终结果，调用方应如何判断确认是否真正成功？
- [ ] 状态 12 的新报价如果也过期，`PayoutRequoteTimeoutOrchestrator` 会把订单推到什么状态？

**结论**：无独立重报价接口；过期对 OpenAPI 不可感知（只能自算 `validUntil`）；重报价靠 confirm 被动触发，链路实测可用。是否作为正式方式待 UPay 答复。

---

## 3. KYC 材料上传的 `fileType` 应该传多少？

**现象**：`payer/dynamic/form` 中两个上传字段要求 `fileType=10`，但上传接口拒绝 `fileType=10` 的所有图片；按我方理解应为 37，但接口不认 37。

**表单来源**（`POST /api/v1/payout/payer/dynamic/form` 响应节选）

```json
{"fieldName":"Proof image","fieldKey":"proofImage","fieldType":"upload","format":"jpg,png,jpeg",
 "description":" Upload files via the /v1/common/file/upload endpoint, fileType=10. Supported formats: jpg, png, jpeg.","required":true}
{"fieldName":"Materia file","fieldKey":"materiaImage","fieldType":"upload","format":"jpg,png,jpeg,pdf",
 "description":"Upload files via the /v1/common/file/upload endpoint, fileType=10. Supported formats: jpg, png, jpeg.","required":true}
```

**上传请求**

```
POST https://openapi.upay-test.best/api/v1/common/file/upload
Content-Type: multipart/form-data; boundary=…
X-UPA-REQUESTID / X-UPA-TIMESTAMP / X-UPA-APIKEY / X-UPA-SIGN
签名原文: POST|/api/v1/common/file/upload|<ts>|<requestId>|        （参数不参与签名）

--boundary
Content-Disposition: form-data; name="file"; filename="w.jpg"
Content-Type: image/jpeg

<826,734 字节的 JPEG>
--boundary
Content-Disposition: form-data; name="fileType"

<取值见下表>
--boundary--
```

**实测结果**（同一文件、同一请求，只改 `fileType`）

| fileType | 文件 | 响应 |
|---|---|---|
| 1 | JPEG / PNG | `{"code":0,"data":{"fileId":"07f79f796e394bd7a4ecd1bf864238c0","fileType":1}}` ✅ |
| 2、3、6、7、8、9 | PNG / JPEG | 均 `code:0` ✅ |
| **10** | JPEG | `{"code":10124,"msg":"Image format error, supports image formats: jpg/png/jpeg"}` ❌ |
| **10** | PNG | 同上 ❌ |
| **10** | PDF（`application/pdf`） | 同上 ❌ |
| 11、12、20 | PNG | `{"code":10301,"msg":"Parameter 'fileType' value error"}` ❌ |
| **37** | JPEG | `{"code":10301,"msg":"Parameter 'fileType' value error"}` ❌ |

**临时方案**：用 `fileType=1` 上传拿到的 fileId 填入 `proofImage` / `materiaImage`，`payer/add` 可以成功。

**补测（用真实截图 1206×2622 PNG，568 KB）**：

| 时间（UTC+4） | X-UPA-REQUESTID | fileType | 响应 |
|---|---|---|---|
| 11:55:12 | `probe1790322912255547200` | 10 | `10124` Image format error |
| 11:55:14 | `probe1790322914975159500` | 37 | `10301` Parameter 'fileType' value error |
| 11:55:17 | `probe1790322917472362000` | 1 | ✅ `fileId=9b52b84160494b09a6b6f579ee14e5ff` |

**源码根因（`upay-open-api@feature/260910-payout` 48dbea88，`upay-common@feature/260910-payout` 220326fb）**

1. `CommonController.upload` 的校验顺序：`AgentFileTypeEnum.getEnumByCode(fileType)` → 后缀必须在 `fileTypeEnum.getFileTypes()` 中，否则抛 `IMAGE_ERROR`(10124) → 大小
2. `AgentFileTypeEnum`：
   ```java
   PAYOUT(10, "汇款", FileTypeEnum.PAYOUT.getName(), true, 10.0, new ArrayList<>());
   ```
   **允许格式是空列表** → `noneMatch` 对空列表恒为 `true` → 任何文件都报 10124
3. `FileUploadReq.fileType` 注释写的是「10=汇款相关文件(大小:10M,格式:需根据字段返回的格式进行处理)」，但 controller 并没有按字段 `format` 校验，只查枚举里的列表
4. **37 不是接口参数**：`FileTypeEnum.PAYOUT(37, "payout")` 是内部的 S3 存储路径枚举（且与 `FIAT_CERTIFICATE(37, …)` 编码重复）；接口参数用的是 `AgentFileTypeEnum`，只有 1–10，所以 37 被 `@ValidateEnum` 拦成 10301

**修复建议**：`PAYOUT(10)` 的 `fileTypes` 补 `JPEG, JPG, PNG, PDF`（与表单 `format` 一致：`proofImage=jpg,png,jpeg`、`materiaImage=jpg,png,jpeg,pdf`），或列表为空时跳过后缀校验。

**请 UPay 确认**
- [x] ~~正确的 `fileType` 是多少？~~ → **10**（`AgentFileTypeEnum.PAYOUT`）；37 是内部存储路径枚举，不是接口参数
- [x] ~~`fileType=10` 为何报 `10124`？~~ → `PAYOUT(10)` 允许格式列表为空
- [x] ~~修复排期~~ → UPay 已处理
- [ ] 修复前用 `fileType=1` 建的测试付款人（`2103372212922290176`）是否需要重建？（仅测试数据）

**复测（UPay 修复后，2026-09-25 UTC+4）**

| 时间 | X-UPA-REQUESTID | 文件 | fileType | 响应 |
|---|---|---|---|---|
| 13:27:15 | `probe1790328435198268500` | PNG 1206×2622（568 KB） | 10 | ✅ `fileId=a2ff2ccf42c74327ba92639a2a8f5f87` |
| 13:27:18 | `probe1790328438380917500` | JPEG（807 KB） | 10 | ✅ `fileId=258e49196c854254835d6b9564b4e9f8` |
| 13:27:21 | `probe1790328441834108500` | PDF | 10 | ✅ `fileId=92e2fa74dd2a4dd1b78a8ddc784d519b` |
| 13:27:35 | — | `payer/add`：`materiaImage`=上面的 PDF，`proofImage`=上面的 PNG | — | ✅ `payerNo=2103416109408985088` |

**结论**：✅ **已修复**。KYC 材料上传传 `fileType=10`，jpg / png / pdf 均可，fileId 可被 `payer/add` 接受。

---

## 4. 银行账户币种字段没有选项

**现象**：`bank/account/dynamic/form` 中 `currency` 字段引用了 `optionKey=bank.bankAccount.default.currency`，但响应的 `options` 里没有这个 key。

**请求**

```
POST https://openapi.upay-test.best/api/v1/payout/bank/account/dynamic/form

Content-Type:    application/json
X-UPA-REQUESTID: probe1790319870403519100
X-UPA-TIMESTAMP: 1790319870403
X-UPA-APIKEY:    01aeea15-****-****-****-a4feda13184f
X-UPA-SIGN:      WOBVvQqOXvKCfFssl5coBXa6lrlzksJTHc5chB84jWs=
签名原文:         POST|/api/v1/payout/bank/account/dynamic/form|1790319870403|probe1790319870403519100|

（请求体为空）
```

**响应**（HTTP 200，节选；完整见 [assets/payout/forms/form_bank_account.json](assets/payout/forms/form_bank_account.json)）

```json
{
  "code": 0, "msg": "Success", "success": true, "ts": 1790319869,
  "data": {
    "forms": [{ "code": "bankAccount", "group": [ …,
      { "code": "default", "fields": [ …,
        { "fieldName": "Account Currency", "fieldKey": "currency", "fieldType": "select",
          "optionKey": "bank.bankAccount.default.currency",
          "format": null, "description": null, "isEdit": true, "required": true },
      … ]}]}],
    "options": {
      "bank.bankAccount.default.transferType": [{"label": "SWIFT", "value": "swift"}],
      "bank.bankAccount.bankClearingCode.type": [{"label": "SWIFT", "value": "swift_code"}]
    }
  }
}
```

**实测**：固定传 `"currency":"USD"` 可以创建银行账户（`bankAccountNo=2103372468649005056`）。

**源码根因（`upay-payout-server` `PayoutSystemFieldServiceImpl` 获取动态表单）**

```java
// 字段层：OpenAPI 只隐藏「国家 / 区号」的 optionKey，币种字段的 optionKey 照常下发
if (!Lists.newArrayList(COUNTRY.getCode(), CALLING_CODE.getCode()).contains(e.getOptionType())) {
    fieldMap.put("optionKey", … e.getFieldCode());          // → "bank.bankAccount.default.currency"
}
// 选项层：国家、区号、法币三类选项整体包在 !isOpenApi 里
if (!isOpenApi) {
    … options.put("country", …); options.put("callingCode", …);
    … options.put(CURRENCY.getCode(), getOptionsByCurrency(language));   // 注意 key 是 "currency"，不是字段 optionKey
}
```

- OpenAPI 调用时：币种字段有 `optionKey`，但 `options` 里没有对应数据 → 就是我们看到的「引用了但没返回」
- 即使非 OpenAPI，options 的 key 是 `"currency"`，与字段下发的 `optionKey`（`bank.bankAccount.default.currency`）也对不上
- 币种数据源：`UserAssetMapper.getCurrencyList` = `sys_country_currency` 表全部 `currency_symbol` 去重；提交时的校验（`validOptions`）也用这份列表

**OpenAPI 现有接口能否替代**

| 接口 | 结果 |
|---|---|
| `POST /api/v1/common/area/list` | ✅ 可调，157 个国家，字段 `type/id/nameZh/nameEn/value/parentId/alpha3/alpha2/children`，**无币种、无电话区号** |
| `POST /api/v1/account/supportcurrency` | ❌ `403 Forbidden Access`（本商户无权限；且属于钱包/卡业务） |
| `POST /api/v1/merchant/fiat/asset` | ❌ `401 Unauthorized` |

结论：**OpenAPI 没有提供汇款币种选项的接口**。顺带发现：付款人 / 收款人表单的「电话区号」（`callingCode`）在 OpenAPI 下同样没有选项来源（被隐藏了 optionKey，`area/list` 也没有区号字段）。

**请 UPay 确认 / 修复**
- [ ] OpenAPI 的动态表单补上币种选项（与字段 `optionKey` 对齐），或新增币种列表接口 / 在 `area/list` 中返回国家对应币种
- [ ] 电话区号的选项来源（`area/list` 补 `tel` 字段即可，`sys_country` 表已有）
- [ ] Swift 汇款实际支持哪些账户币种？（`sys_country_currency` 是全量法币，渠道未必都支持）

**结论**：❌ OpenAPI 无币种选项来源，属 UPay 缺陷（`!isOpenApi` 分支遗漏 + options key 不一致）。我方当前固定 `USD`（实测可建账户），区号用前端内置静态表。

---

## 5. 🐞 已取消的订单可以通过 `confirm` 被「复活」

**现象**：对已取消（状态 9）的订单，用其过期报价调 `order/confirm`，接口返回成功，订单被改回 `3`，随后重报价进入 `12`，拿到新的可用报价 —— 已取消订单重新进入可确认汇款的状态。

**订单**：`thirdOrderNo=PO1790318127PROBE`，`orderNo=2026092502103372790935130112`，20 USD / Swift

**时间线**（UTC+4；括号内为毫秒时间戳）

| 时间 | 事件 | 来源 |
|---|---|---|
| 2026-09-25 10:35:27.000 | 订单创建，状态 0 | `crateAt=1790318127000` |
| 10:35:27.097 | 原报价生成 `Q1790318127097b1d1e66cc535`（状态 2） | quoteNo 内嵌毫秒 |
| 10:38:27.000 | 原报价过期（订单仍为 2、报价仍为 3） | `validUntil=1790318307000` |
| 10:39:00 之后数十秒内 | `order/cancel` → 返回 `status=9` | 我方未记录精确时间，以 UPay `payout_order_log` 为准 |
| 11:43:38 | `order/info` 确认状态为 **9** | 我方日志 |
| **11:43:39.596** | `order/confirm`（旧 quoteNo）→ 返回 `{"orderStatus":3,"quoteNo":"Q1790318127097b1d1e66cc535"}` | 请求发出时间 `1790322219596` |
| 11:43:39.010 | 新报价生成 `Q1790322219010a7a619ee4909`，批次 `QB1790322218999c3c5c1534fea` | quoteNo 内嵌毫秒（与我方时钟差 < 1 秒） |
| 11:43:50 起 | `quote/info`：订单 **12**，新报价状态 3、`debitAmount=68.06` | 我方日志，至 11:46:02 持续为 12 |
| 11:46:39.000 | 新报价过期 | `validUntil=1790322399000` |

**请求**

```
POST https://openapi.upay-test.best/api/v1/payout/order/confirm
明文参数（加密前）：{"thirdOrderNo":"PO1790318127PROBE","quoteNo":"Q1790318127097b1d1e66cc535"}
```

**响应**（解密后）：`{"orderNo":"2026092502103372790935130112","orderStatus":3,"quoteNo":"Q1790318127097b1d1e66cc535"}`

**资金**：前后商户 USDT `balance=465`、`frozen=12.12` 均未变化 —— 仅复活到 12，**未冻结、未出款**（新报价未确认）。

**根因（源码）**
- open-api `confirmRemit`：只查订单、报价是否存在（`findByBatchNoAndQuoteNo`），不查订单状态
- payout `PayoutOrderServiceImpl.confirmRemit:391`：不查订单状态、不查报价状态、不查有效期，直接 `status=QUOTE_CONFIRMED` 并发 MQ
- MQ 消费 `PayoutOrderConfirmationState.shouldProcess` 只判断 `status == 3`，于是继续走重报价
- 对比：`PayoutOrderOrchestrator.confirmQuote:125` 有「仅 2 / 12 可确认」「报价须为 AVAILABLE」「有效期」校验，但 OpenAPI 与商户后台（`upay-broker-api` `PayoutOrderController.confirm`）都没走这条

**风险**：若报价仍在有效期内，对已取消订单调 confirm 会直接冻结资产并向渠道下单（本次因报价已过期，停在 12）。

**请 UPay 确认 / 修复**
- [ ] `confirmRemit` 增加订单状态校验（仅 2、12 可确认）与报价状态、有效期校验，或改为调用 `confirmQuote`
- [ ] 该测试订单当前停在 12，是否需要 UPay 侧清理

**结论**：

---

## 附：其他已发现的差异（顺带确认，非阻塞）

| # | 差异 | 实测 | 当前处理 |
|---|---|---|---|
| A | IBAN 对非 IBAN 国家（如美国）也必填 | 不传返回 `8002 IBAN is required`；传英国 IBAN + 美国银行也能创建（不校验国家） | UI 必填；请确认是否应按国家可选 |
| B | 汇款金额下限 | 文档写最小 0.01；实测 2 USD 返回 `8009 Remittance amount exceeds the limit, allowed range: 5 ~ 100000` | 按 5 ~ 100000 校验；请更新文档 |
| C | `debitCoinId` 取值 | 文档示例 `"USDT"`；`debit/coin/list` 实际返回 `{"coinId":"458884","symbol":"USDT"}` | 传数字 ID |
| D | 下单后 `orderStatus=0` | 文档状态枚举从 1 开始 | 按「报价中」处理；请补充文档 |
| E | 时间字段格式 | 文档写 ISO 字符串（`"2026-09-24T10:00:00Z"`），实际为毫秒时间戳字符串（`"1790318127000"`） | 按 ms 解析；请更新文档 |
| F | `quoteTime` 比实际快 8 小时 | `quoteTime=1790346927000`，`crateAt=1790318127000`，相差 28800 秒 | 不使用该字段 |
| G | 报价里有文档未列字段 | `channelCurrency`、`channelTotalAmount`、`batchNo`、`quoteId`、`kycUrl`；`remitMethod` 为字符串 `"swift"`（文档写 int） | 宽松解析；请补充含义 |
| H | 新建主体 `dataCompleteStatus=2` | 付款人、银行账户所有必填项都填了，仍为「部分缺失」 | 不阻塞；请说明缺失判定依据 |
| I | ~~回调签名口径~~ | ✅ 已由源码确认（`upay-broker-server` `WebhookConsumer.post:241`）：`event="PO_ORDER_STATUS_CHANGE"`，`body` 为 JWE 密文原串，key 为 `agent_setting.secret_key`（与 API 签名同一把） | 按此实现，无需实测 |

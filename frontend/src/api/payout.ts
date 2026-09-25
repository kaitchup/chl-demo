import api from "./client";

// Errors from /api/payout carry a stable code plus optional details.
export class PayoutApiError extends Error {
  constructor(public code: string, message: string, public details: Record<string, any> = {}) {
    super(message);
  }
}

function unwrap<T>(p: Promise<any>): Promise<T> {
  return p.then(
    (r) => r.data.data as T,
    (err) => {
      const e = err?.response?.data?.error;
      if (e) throw new PayoutApiError(e.code, e.message, e.details || {});
      throw new PayoutApiError("NETWORK", err?.message || "网络错误");
    }
  );
}

export interface Bootstrap {
  has_payer: boolean;
  payer_name: string;
  has_trade_password: boolean;
  debit_symbol: string;
  min_amount: string;
  max_amount: string;
  currency: string;
}

export interface Country {
  code: string;
  name_zh: string;
  name_en: string;
}

export interface Options {
  countries: Country[];
  genders: string[];
  doc_types: string[];
  expiry_types: string[];
  employment_status: string[];
  sources_of_income: string[];
}

export interface Recipient {
  id: number;
  name: string;
  given_name: string;
  family_name: string;
  bank_name: string;
  swift_code: string;
  account_last4: string;
  currency: string;
  bank_country: string;
  complete: boolean;
  last_error: string | null;
}

export interface PayerInput {
  given_name: string;
  family_name: string;
  name_local: string;
  date_of_birth: string;
  nationality: string;
  gender: string;
  email: string;
  calling_code: string;
  phone: string;
  doc_type: string;
  doc_number: string;
  doc_issuing_country: string;
  doc_issued_date: string;
  doc_expiry_type: string;
  doc_expiry_date: string;
  identity_file_id: string;
  identity_file_name: string;
  addr_country: string;
  addr_province: string;
  addr_city: string;
  addr_line1: string;
  addr_line2: string;
  addr_postal_code: string;
  address_proof_file_id: string;
  address_proof_file_name: string;
  employment_status: string;
  source_of_income: string;
  occupation: string;
}

export interface BankInput {
  bank_country: string;
  bank_name: string;
  swift_code: string;
  account_number: string;
  iban: string;
}

export interface RecipientInput {
  given_name: string;
  family_name: string;
  date_of_birth: string;
  nationality: string;
  email: string;
  calling_code: string;
  phone: string;
  addr_country: string;
  addr_state: string;
  addr_city: string;
  addr_line1: string;
  addr_line2: string;
  addr_postal_code: string;
  bank: BankInput;
}

export type Stage =
  | "quoting"
  | "awaiting_confirm"
  | "quote_failed"
  | "paid"
  | "processing"
  | "completed"
  | "failed"
  | "canceled"
  | "refunding"
  | "refunded"
  | "unknown";

export interface Quote {
  quote_no: string;
  debit_coin: string;
  debit_total: string;
  pay_amount: string;
  fixed_fee: string;
  transaction_fee: string;
  exchange_fee: string;
  fee_currency: string;
  destination_amount: string;
  destination_currency: string;
  valid_until: number;
  kyc_url?: string;
}

export interface QuoteState {
  status: number;
  stage: Stage;
  quote: Quote | null;
  expires_in: number;
}

export interface CreatedOrder {
  id: number;
  status: number;
  stage: Stage;
  information_to_improve: any;
}

export interface OrderSummary {
  id: number;
  amount: string;
  currency: string;
  debit_coin: string;
  debit_total: string;
  recipient_name: string;
  status: number;
  stage: Stage;
  created_at: string;
}

export interface OrderDetail {
  id: number;
  third_order_no: string;
  order_no: string | null;
  amount: string;
  currency: string;
  debit_coin: string;
  usage: string;
  note: string;
  remit_method: string;
  status: number;
  stage: Stage;
  quote: Quote | null;
  exchange_rate: string;
  recipient: Recipient;
  timeline: { status: number; stage: Stage; at: string; source: string }[];
  last_error: string | null;
  created_at: string;
  confirmed_at: string | null;
}

export const payoutApi = {
  bootstrap: () => unwrap<Bootstrap>(api.get("/payout/bootstrap")),
  options: () => unwrap<Options>(api.get("/payout/options")),
  upload: (file: File) => {
    const fd = new FormData();
    fd.append("file", file);
    return unwrap<{ file_id: string; file_name: string }>(api.post("/payout/files", fd));
  },
  createPayer: (input: PayerInput) => unwrap<{ name: string }>(api.post("/payout/payer", input)),
  recipients: () => unwrap<Recipient[]>(api.get("/payout/recipients")),
  createRecipient: (input: RecipientInput) => unwrap<Recipient>(api.post("/payout/recipients", input)),
  retryRecipient: (id: number, bank: BankInput) =>
    unwrap<Recipient>(api.post(`/payout/recipients/${id}/retry`, bank)),
  setTradePassword: (password: string) => unwrap<{}>(api.post("/payout/trade-password", { password })),
  createOrder: (input: { recipient_id: number; amount: string; usage: string; note: string }) =>
    unwrap<CreatedOrder>(api.post("/payout/orders", input)),
  orders: (beforeId?: number) =>
    unwrap<{ orders: OrderSummary[]; has_more: boolean }>(
      api.get("/payout/orders", { params: beforeId ? { before_id: beforeId } : {} })
    ),
  order: (id: string | number) => unwrap<OrderDetail>(api.get(`/payout/orders/${id}`)),
  quote: (id: string | number) => unwrap<QuoteState>(api.get(`/payout/orders/${id}/quote`)),
  confirm: (id: string | number, quote_no: string, trade_password: string) =>
    unwrap<{ status: number; stage: Stage }>(api.post(`/payout/orders/${id}/confirm`, { quote_no, trade_password })),
  cancel: (id: string | number) => unwrap<{ status: number; stage: Stage }>(api.post(`/payout/orders/${id}/cancel`)),
  requote: (id: string | number) => unwrap<CreatedOrder>(api.post(`/payout/orders/${id}/requote`)),
};

// ---- display helpers ----

export const DOC_TYPE_LABEL: Record<string, string> = {
  passport: "护照",
  national_id: "身份证",
  driving_license: "驾照",
  residence_permit: "居留许可",
};

export const GENDER_LABEL: Record<string, string> = { Male: "男", Female: "女" };

export const EMPLOYMENT_LABEL: Record<string, string> = {
  employed: "受雇",
  self_employed: "自雇",
  unemployed: "待业",
  student: "学生",
  retired: "退休",
  homemaker: "家庭主妇/主夫",
  other: "其他",
};

export const INCOME_LABEL: Record<string, string> = {
  Salary: "工资/薪金收入",
  Pension: "养老金",
  Investment: "投资收益",
  Property: "房产收入",
  FriendsAndFamily: "亲友资助",
  Benefits: "福利补贴",
};

export const USAGE_PRESETS = ["Capital transfer", "Family support", "Salary", "Tuition", "Goods purchase"];

export const STAGE_LABEL: Record<Stage, string> = {
  quoting: "报价中",
  awaiting_confirm: "待确认",
  quote_failed: "报价失败",
  paid: "支付已收到",
  processing: "银行处理中",
  completed: "转账已转出",
  failed: "汇款失败",
  canceled: "已取消",
  refunding: "退款处理中",
  refunded: "已退款",
  unknown: "未知状态",
};

// User-facing message for known error codes; falls back to the server message.
export function errorText(e: unknown): string {
  if (!(e instanceof PayoutApiError)) return (e as Error)?.message || "操作失败";
  switch (e.code) {
    case "VALIDATION":
      return `请检查「${e.details.field ?? ""}」：${e.message.split(": ").slice(1).join(": ")}`;
    case "AMOUNT_OUT_OF_RANGE":
      return `汇出金额需在 ${e.details.min} ~ ${e.details.max} 之间`;
    case "FILE_EXPIRED":
      return "上传的文件已过期，请重新上传";
    case "FILE_TOO_LARGE":
      return "文件不能超过 10MB";
    case "QUOTE_EXPIRED":
      return "报价已过期，请重新获取报价";
    case "QUOTE_CHANGED":
      return "报价已变化，请刷新后重试";
    case "TRADE_PASSWORD_INVALID":
      return `交易密码错误，还可尝试 ${e.details.remaining_attempts} 次`;
    case "TRADE_PASSWORD_LOCKED":
      return `错误次数过多，请 ${Math.ceil((e.details.retry_after ?? 0) / 60)} 分钟后再试`;
    case "PAYOUT_NOT_ENABLED":
      return "当前账户未开通汇款功能";
    case "UPSTREAM":
      return `汇款服务返回错误：${e.message}${e.details.upay_code ? `（${e.details.upay_code}）` : ""}`;
    default:
      return e.message;
  }
}

export function fmtTime(s: string | number | null | undefined): string {
  if (!s) return "";
  const d = new Date(s);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

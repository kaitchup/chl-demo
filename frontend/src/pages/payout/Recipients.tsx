import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { BankInput, PayoutApiError, RecipientInput, payoutApi } from "../../api/payout";
import {
  BankAvatar,
  Button,
  CountrySelect,
  ErrorBanner,
  Field,
  PayoutLayout,
  TextInput,
  Title,
} from "../../components/payout/ui";

export function Recipients() {
  const nav = useNavigate();
  const { data, error, isLoading } = useQuery({ queryKey: ["payout-recipients"], queryFn: payoutApi.recipients });
  const [selected, setSelected] = useState<number | null>(null);
  const list = data ?? [];
  const chosen = selected ?? list.find((r) => r.complete)?.id ?? null;

  return (
    <PayoutLayout
      back="/"
      close="/"
      right={
        <Link to="/payout/recipients/new" className="text-xs bg-emerald-500 text-white rounded px-2 py-1">
          添加收款人
        </Link>
      }
      footer={
        <>
          <Button variant="secondary" onClick={() => nav("/payout/orders")}>
            汇款记录
          </Button>
          <Button disabled={chosen === null} onClick={() => nav(`/payout/new?recipient=${chosen}`)}>
            继续
          </Button>
        </>
      }
    >
      <Title>选择收款人</Title>
      <ErrorBanner error={error} />
      {isLoading && <p className="text-sm text-slate-500">加载中…</p>}
      {!isLoading && list.length === 0 && (
        <div className="text-center text-sm text-slate-500 py-16">
          还没有收款人，
          <Link to="/payout/recipients/new" className="text-emerald-600">
            添加一个
          </Link>
        </div>
      )}
      <div className="space-y-3">
        {list.map((r) =>
          r.complete ? (
            <button
              key={r.id}
              onClick={() => setSelected(r.id)}
              className={`w-full flex items-center gap-3 rounded-xl border px-3 py-3 text-left ${
                chosen === r.id ? "border-emerald-500 bg-emerald-50/40" : "border-slate-200"
              }`}
            >
              <BankAvatar />
              <span className="flex-1 min-w-0">
                <span className="block">{r.name}</span>
                <span className="block text-xs text-slate-500 truncate">
                  {r.swift_code} · **** {r.account_last4}
                </span>
              </span>
              <span
                className={`w-5 h-5 rounded-full border-2 ${chosen === r.id ? "border-emerald-500 bg-[radial-gradient(circle,theme(colors.emerald.500)_40%,white_45%)]" : "border-slate-300"}`}
              />
            </button>
          ) : (
            <Link
              key={r.id}
              to={`/payout/recipients/new?retry=${r.id}`}
              className="w-full flex items-center gap-3 rounded-xl border border-dashed border-amber-300 px-3 py-3"
            >
              <BankAvatar />
              <span className="flex-1 min-w-0">
                <span className="block">{r.name}</span>
                <span className="block text-xs text-amber-600">银行账户未完成 · 点击重试</span>
              </span>
            </Link>
          )
        )}
      </div>
    </PayoutLayout>
  );
}

const emptyBank: BankInput = { bank_country: "", bank_name: "", swift_code: "", account_number: "", iban: "" };
const empty: Omit<RecipientInput, "bank"> = {
  given_name: "", family_name: "", date_of_birth: "", nationality: "", email: "", calling_code: "", phone: "",
  addr_country: "", addr_state: "", addr_city: "", addr_line1: "", addr_line2: "", addr_postal_code: "",
};

// /payout/recipients/new — one page, three sections. With ?retry=<id> only the
// bank section is shown (the beneficiary already exists at UPay).
export function RecipientNew() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const retryId = Number(params.get("retry")) || null;
  const [f, setF] = useState(empty);
  const [b, setB] = useState<BankInput>(emptyBank);
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof empty) => (v: string) => setF((s) => ({ ...s, [k]: v }));
  const setBank = (k: keyof BankInput) => (v: string) => setB((s) => ({ ...s, [k]: v }));

  const bankOk = Object.values(b).every((v) => v.trim() !== "");
  const personOk = Object.entries(f).every(([k, v]) => k === "addr_line2" || v.trim() !== "");
  const canSave = bankOk && (retryId !== null || personOk);

  const save = async () => {
    setErr(null);
    setBusy(true);
    try {
      if (retryId) await payoutApi.retryRecipient(retryId, b);
      else await payoutApi.createRecipient({ ...f, bank: b });
      await qc.invalidateQueries({ queryKey: ["payout-recipients"] });
      nav("/payout/recipients", { replace: true });
    } catch (e) {
      setErr(e);
      // Beneficiary created but bank failed: continue as a retry of that record.
      if (e instanceof PayoutApiError && e.code === "RECIPIENT_BANK_FAILED" && e.details.recipient_id) {
        await qc.invalidateQueries({ queryKey: ["payout-recipients"] });
        nav(`/payout/recipients/new?retry=${e.details.recipient_id}`, { replace: true });
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <PayoutLayout back="/payout/recipients" close={false} footer={<Button onClick={save} disabled={!canSave || busy}>{busy ? "保存中…" : "保存"}</Button>}>
      <Title>{retryId ? "补全银行账户" : "添加收款人"}</Title>
      <div className="flex gap-3 rounded-xl bg-slate-50 p-3 mb-6">
        <span className="flex-1 h-11 rounded-lg border bg-white flex items-center px-3 gap-2">🇺🇸 USD</span>
        <span className="flex-1 h-11 rounded-lg border bg-white flex items-center px-3 gap-2">🌐 Swift</span>
      </div>
      <ErrorBanner error={err} />

      {!retryId && (
        <>
          <h2 className="text-sm text-slate-500 mb-3">收款人信息</h2>
          <div className="flex gap-3">
            <div className="flex-1">
              <Field label="名" required>
                <TextInput value={f.given_name} onChange={set("given_name")} placeholder="名（英文）" />
              </Field>
            </div>
            <div className="flex-1">
              <Field label="姓" required>
                <TextInput value={f.family_name} onChange={set("family_name")} placeholder="姓（英文）" />
              </Field>
            </div>
          </div>
          <Field label="出生日期" required>
            <TextInput type="date" value={f.date_of_birth} onChange={set("date_of_birth")} />
          </Field>
          <Field label="国籍" required>
            <CountrySelect value={f.nationality} onChange={set("nationality")} placeholder="请选择国籍" />
          </Field>
          <Field label="电子邮箱" required>
            <TextInput type="email" inputMode="email" value={f.email} onChange={set("email")} placeholder="电子邮箱" />
          </Field>
          <div className="flex gap-3">
            <div className="w-28">
              <Field label="区号" required>
                <TextInput value={f.calling_code} onChange={set("calling_code")} placeholder="+1" inputMode="tel" />
              </Field>
            </div>
            <div className="flex-1">
              <Field label="电话" required>
                <TextInput value={f.phone} onChange={set("phone")} placeholder="电话号码" inputMode="tel" />
              </Field>
            </div>
          </div>

          <h2 className="text-sm text-slate-500 mb-3 mt-2">收款人地址</h2>
          <Field label="国家或地区" required>
            <CountrySelect value={f.addr_country} onChange={set("addr_country")} />
          </Field>
          <div className="flex gap-3">
            <div className="flex-1">
              <Field label="省/州" required>
                <TextInput value={f.addr_state} onChange={set("addr_state")} />
              </Field>
            </div>
            <div className="flex-1">
              <Field label="城市" required>
                <TextInput value={f.addr_city} onChange={set("addr_city")} />
              </Field>
            </div>
          </div>
          <Field label="详细地址" required>
            <TextInput value={f.addr_line1} onChange={set("addr_line1")} maxLength={200} />
          </Field>
          <Field label="地址补充">
            <TextInput value={f.addr_line2} onChange={set("addr_line2")} placeholder="选填" maxLength={200} />
          </Field>
          <Field label="邮编" required>
            <TextInput value={f.addr_postal_code} onChange={set("addr_postal_code")} maxLength={20} />
          </Field>
        </>
      )}

      <h2 className="text-sm text-slate-500 mb-3 mt-2">银行信息</h2>
      <Field label="银行所在国家或地区" required>
        <CountrySelect value={b.bank_country} onChange={setBank("bank_country")} />
      </Field>
      <Field label="银行名称" required>
        <TextInput value={b.bank_name} onChange={setBank("bank_name")} placeholder="银行名称" maxLength={140} />
      </Field>
      <Field label="Swift代码" required>
        <TextInput value={b.swift_code} onChange={setBank("swift_code")} placeholder="Swift代码" maxLength={11} />
      </Field>
      <Field label="银行账号" required>
        <TextInput value={b.account_number} onChange={setBank("account_number")} placeholder="银行账号" maxLength={34} />
      </Field>
      <Field label="IBAN" required hint="汇款服务要求必填；所在国不使用 IBAN 时请联系客服">
        <TextInput value={b.iban} onChange={setBank("iban")} placeholder="IBAN" maxLength={34} />
      </Field>
    </PayoutLayout>
  );
}

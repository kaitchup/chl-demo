import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";
import { USAGE_PRESETS, payoutApi } from "../../api/payout";
import { BankAvatar, Button, ErrorBanner, Field, PayoutLayout, TextArea, TextInput, Title } from "../../components/payout/ui";
import { useBootstrap } from "./Entry";

// /payout/new?recipient=<id>[&amount=&usage=] — "汇款金额与用途" (design 9).
export default function Amount() {
  const nav = useNavigate();
  const [params] = useSearchParams();
  const recipientId = Number(params.get("recipient"));
  const { data: boot } = useBootstrap();
  const { data: recipients } = useQuery({ queryKey: ["payout-recipients"], queryFn: payoutApi.recipients });
  const recipient = recipients?.find((r) => r.id === recipientId);

  const [amount, setAmount] = useState(params.get("amount") ?? "");
  const [usage, setUsage] = useState(params.get("usage") ?? "");
  const [note, setNote] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const amountOk = /^\d+(\.\d{1,2})?$/.test(amount) && Number(amount) >= Number(boot?.min_amount ?? 5) &&
    Number(amount) <= Number(boot?.max_amount ?? 100000);
  const canSubmit = !!recipient && amountOk && usage.trim() !== "";

  const submit = async () => {
    setErr(null);
    setBusy(true);
    try {
      const o = await payoutApi.createOrder({ recipient_id: recipientId, amount, usage: usage.trim(), note: note.trim() });
      nav(`/payout/orders/${o.id}/confirm`);
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <PayoutLayout
      back="/payout/recipients"
      close="/payout/recipients"
      footer={
        <>
          <Button variant="secondary" onClick={() => nav("/payout/recipients")}>
            上一步
          </Button>
          <Button onClick={submit} disabled={!canSubmit || busy}>
            {busy ? "获取中…" : "获取报价"}
          </Button>
        </>
      }
    >
      <Title>汇款金额与用途</Title>
      <ErrorBanner error={err} />
      {recipient && (
        <div className="flex items-center gap-3 rounded-xl bg-slate-50 px-3 py-3 mb-5">
          <BankAvatar />
          <span className="min-w-0">
            <span className="block">{recipient.name}</span>
            <span className="block text-xs text-slate-500">
              {recipient.swift_code} · **** {recipient.account_last4}
            </span>
          </span>
        </div>
      )}
      <Field label="扣款资产" required>
        <div className="h-11 border border-slate-200 rounded-lg px-3 flex items-center gap-2 text-sm">
          <span className="w-5 h-5 rounded-full bg-emerald-500 text-white text-[11px] flex items-center justify-center">₮</span>
          {boot?.debit_symbol ?? "USDT"}
        </div>
      </Field>
      <Field
        label={`汇出法币金额（${recipient?.currency ?? "USD"}）`}
        required
        hint={`单笔 ${boot?.min_amount ?? 5} ~ ${boot?.max_amount ?? 100000}，最多两位小数`}
      >
        <TextInput value={amount} onChange={setAmount} placeholder="0.00" inputMode="decimal" />
      </Field>
      <Field label="汇款用途" required>
        <TextInput value={usage} onChange={setUsage} placeholder="请输入" maxLength={140} />
      </Field>
      <div className="-mt-2 mb-4 flex flex-wrap gap-2">
        {USAGE_PRESETS.map((u) => (
          <button
            key={u}
            onClick={() => setUsage(u)}
            className={`text-xs rounded-full border px-2.5 py-1 ${usage === u ? "border-emerald-500 text-emerald-600" : "border-slate-200 text-slate-500"}`}
          >
            {u}
          </button>
        ))}
      </div>
      <Field label="备注">
        <TextArea value={note} onChange={setNote} maxLength={140} />
      </Field>
    </PayoutLayout>
  );
}

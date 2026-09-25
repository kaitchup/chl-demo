import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, useNavigate, useParams } from "react-router-dom";
import { payoutApi } from "../../api/payout";
import {
  BankAvatar,
  Button,
  ErrorBanner,
  PayoutLayout,
  PinSheet,
  Title,
  mmss,
  useCountdown,
} from "../../components/payout/ui";
import { useBootstrap } from "./Entry";

const QUOTE_POLL_MS = 2000;
const QUOTE_WAIT_MS = 60_000; // give up waiting for a quote after a minute

function Row({ k, v, strong }: { k: string; v: string; strong?: boolean }) {
  return (
    <div className={`flex justify-between py-2 ${strong ? "text-base" : "text-sm"}`}>
      <span className="text-slate-500">{k}</span>
      <span className={strong ? "font-semibold text-emerald-600" : ""}>{v}</span>
    </div>
  );
}

// /payout/orders/:id/confirm — "确认并转出" (design 10, 11).
export default function Confirm() {
  const { id } = useParams<{ id: string }>();
  const nav = useNavigate();
  const qc = useQueryClient();
  const started = useRef(Date.now());
  const { data: boot } = useBootstrap();
  const { data: order } = useQuery({ queryKey: ["payout-order", id], queryFn: () => payoutApi.order(id!) });
  const q = useQuery({
    queryKey: ["payout-quote", id],
    queryFn: () => payoutApi.quote(id!),
    refetchInterval: (query) => {
      const s = query.state.data;
      const waiting = !s || s.stage === "quoting" || (s.stage === "awaiting_confirm" && !s.quote);
      return waiting && Date.now() - started.current < QUOTE_WAIT_MS ? QUOTE_POLL_MS : false;
    },
  });
  const state = q.data;
  const quote = state?.quote ?? null;
  const left = useCountdown(quote ? quote.valid_until : null);
  const [agreed, setAgreed] = useState(false);
  const [pin, setPin] = useState<"verify" | "setup" | null>(null);
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => setErr(null), [quote?.quote_no]);

  if (state && ["paid", "processing", "completed", "failed", "refunding", "refunded"].includes(state.stage)) {
    return <Navigate to={`/payout/orders/${id}`} replace />;
  }

  const expired = !!quote && left === 0;
  const timedOut = !quote && state?.stage !== "quote_failed" && Date.now() - started.current >= QUOTE_WAIT_MS;

  const requote = async () => {
    setErr(null);
    setBusy(true);
    try {
      const o = await payoutApi.requote(id!);
      started.current = Date.now();
      nav(`/payout/orders/${o.id}/confirm`, { replace: true });
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  const cancel = async () => {
    setBusy(true);
    try {
      await payoutApi.cancel(id!);
      nav("/payout/recipients", { replace: true });
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  const submitPin = async (pwd: string) => {
    if (pin === "setup") {
      await payoutApi.setTradePassword(pwd);
      await qc.invalidateQueries({ queryKey: ["payout-bootstrap"] });
    }
    await payoutApi.confirm(id!, quote!.quote_no, pwd);
    setPin(null);
    nav(`/payout/orders/${id}`, { replace: true });
  };

  return (
    <PayoutLayout
      back="/payout/recipients"
      close="/payout/recipients"
      footer={
        <>
          <Button variant="secondary" onClick={cancel} disabled={busy}>
            取消订单
          </Button>
          {expired || state?.stage === "quote_failed" || timedOut ? (
            <Button onClick={requote} disabled={busy}>
              {busy ? "获取中…" : "重新获取报价"}
            </Button>
          ) : (
            <Button onClick={() => setPin(boot?.has_trade_password ? "verify" : "setup")} disabled={!quote || !agreed || busy}>
              确认汇款
            </Button>
          )}
        </>
      }
    >
      <Title
        extra={
          quote && (
            <span className={`text-sm rounded-full px-3 py-1 ${expired ? "bg-slate-100 text-slate-500" : "bg-emerald-50 text-emerald-600"}`}>
              {expired ? "报价已过期" : `有效期 ${mmss(left)}`}
            </span>
          )
        }
      >
        确认并转出
      </Title>
      <ErrorBanner error={err ?? q.error} />
      {quote?.kyc_url && (
        <a href={quote.kyc_url} target="_blank" rel="noreferrer" className="block mb-4 rounded-lg bg-amber-50 text-amber-800 text-sm px-3 py-2">
          汇款服务要求补充 KYC 认证，点此完成 →
        </a>
      )}

      <p className="text-sm text-slate-500 mb-2">转账至</p>
      <div className="flex items-center gap-3 rounded-xl bg-slate-50 px-3 py-3 mb-5">
        <BankAvatar />
        <span className="min-w-0">
          <span className="block">{order?.recipient.name ?? "…"}</span>
          <span className="block text-xs text-slate-500">
            {order?.recipient.swift_code} · **** {order?.recipient.account_last4}
          </span>
        </span>
      </div>

      <p className="text-sm text-slate-500 mb-2">转账详情</p>
      <div className="rounded-xl bg-slate-50 px-4 py-2 mb-5">
        {!quote ? (
          <p className="text-sm text-slate-500 py-6 text-center">
            {state?.stage === "quote_failed"
              ? "报价失败，请重新获取报价"
              : timedOut
              ? "暂未获取到报价，请重新获取"
              : "正在获取报价…"}
          </p>
        ) : (
          <>
            <Row k="汇出金额" v={`${quote.destination_amount} ${quote.destination_currency}`} />
            <Row k="支付金额" v={`${quote.pay_amount} ${quote.debit_coin}`} />
            <Row k="固定手续费" v={`${quote.fixed_fee} ${quote.fee_currency}`} />
            <Row k="交易手续费" v={`${quote.transaction_fee} ${quote.fee_currency}`} />
            <Row k="汇兑手续费" v={`${quote.exchange_fee} ${quote.fee_currency}`} />
            <div className="border-t my-1" />
            <Row k="付款总额" v={`${quote.debit_total} ${quote.debit_coin}`} strong />
          </>
        )}
      </div>

      {quote && (
        <>
          <p className="text-sm text-slate-500 mb-2">到账详情</p>
          <div className="rounded-xl bg-slate-50 px-4 py-1 mb-5">
            <Row k="到账" v={`预计 ${quote.destination_amount} ${quote.destination_currency} 到账`} />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={agreed} onChange={(e) => setAgreed(e.target.checked)} className="accent-emerald-500" />
            我已阅读并同意本次报价对应的
            <a href="#refund-policy" className="text-emerald-600" onClick={(e) => e.preventDefault()}>
              退款协议
            </a>
          </label>
        </>
      )}

      {pin && <PinSheet mode={pin} onSubmit={submitPin} onClose={() => setPin(null)} />}
    </PayoutLayout>
  );
}

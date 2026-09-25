import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { OrderDetail as Detail, STAGE_LABEL, Stage, fmtTime, payoutApi } from "../../api/payout";
import { Button, ErrorBanner, PayoutLayout, Title, countryLabel, useOptions } from "../../components/payout/ui";

const TERMINAL: Stage[] = ["completed", "failed", "canceled", "refunded", "quote_failed"];
const FAILED: Stage[] = ["failed", "canceled", "refunding", "refunded", "quote_failed"];

function headline(o: Detail) {
  return o.quote ? `${o.quote.debit_total} ${o.quote.debit_coin}` : `${o.amount} ${o.currency}`;
}

// The design's three-node timeline; failure-type stages add a red final node.
function Timeline({ o }: { o: Detail }) {
  const at = (stages: Stage[]) => o.timeline.find((e) => stages.includes(e.stage))?.at;
  const nodes: { label: string; at?: string; sub?: string; bad?: boolean }[] = [
    { label: "支付已收到", at: at(["paid", "processing", "completed"]) },
    {
      label: "银行处理中",
      at: at(["processing", "completed"]),
      sub: o.quote ? `预计 ${o.quote.destination_amount} ${o.quote.destination_currency} 到账` : undefined,
    },
    { label: "转账已转出", at: at(["completed"]) },
  ];
  if (FAILED.includes(o.stage)) {
    const last = nodes.findIndex((n) => !n.at);
    nodes.splice(last < 0 ? nodes.length : last, nodes.length, {
      label: STAGE_LABEL[o.stage],
      at: at([o.stage]),
      bad: true,
    });
  }
  const current = nodes.findIndex((n) => !n.at);
  return (
    <ol className="rounded-xl bg-slate-50 px-4 py-4">
      {nodes.map((n, i) => {
        const done = !!n.at && !n.bad;
        const active = i === current - 1 && !TERMINAL.includes(o.stage);
        return (
          <li key={i} className="relative pl-7 pb-5 last:pb-0">
            {i < nodes.length - 1 && <span className="absolute left-[7px] top-4 bottom-0 w-px bg-slate-200" />}
            <span
              className={`absolute left-0 top-0.5 w-4 h-4 rounded-full border-2 ${
                n.bad ? "border-red-500 bg-red-500" : active ? "border-amber-400 bg-amber-400" : done ? "border-emerald-500" : "border-slate-200"
              }`}
            />
            {n.at && <div className="text-sm">{fmtTime(n.at)}</div>}
            <div className={`text-sm ${n.at ? (n.bad ? "text-red-600" : "") : "text-slate-400"}`}>{n.label}</div>
            {n.sub && n.at && <div className="text-xs text-slate-500">{n.sub}</div>}
          </li>
        );
      })}
    </ol>
  );
}

function KV({ k, v }: { k: string; v?: string | null }) {
  return (
    <div className="flex justify-between gap-4 py-2 text-sm">
      <span className="text-slate-500 shrink-0">{k}</span>
      <span className="text-right break-all">{v || "--"}</span>
    </div>
  );
}

// /payout/orders/:id — result + detail (design 12–14).
export function OrderDetail() {
  const { id } = useParams<{ id: string }>();
  const nav = useNavigate();
  const [tab, setTab] = useState<"status" | "detail">("status");
  const { data: countries } = useOptions();
  const { data: o, error } = useQuery({
    queryKey: ["payout-order", id],
    queryFn: () => payoutApi.order(id!),
    refetchInterval: (q) => (q.state.data && TERMINAL.includes(q.state.data.stage) ? false : 5000),
  });
  if (!o) return <PayoutLayout close="/payout/orders">{error ? <ErrorBanner error={error} /> : "加载中…"}</PayoutLayout>;

  const again = () =>
    nav(`/payout/new?recipient=${o.recipient.id}&amount=${o.amount}&usage=${encodeURIComponent(o.usage)}`);
  const unconfirmed = o.stage === "quoting" || o.stage === "awaiting_confirm";
  const q = o.quote;

  return (
    <PayoutLayout
      back={false}
      close="/payout/orders"
      footer={
        unconfirmed ? (
          <Button onClick={() => nav(`/payout/orders/${o.id}/confirm`)}>继续确认</Button>
        ) : (
          <Button onClick={again}>重复此次转出</Button>
        )
      }
    >
      <div className="flex flex-col items-center mt-4 mb-6">
        <span className="w-14 h-14 rounded-full bg-slate-100 flex items-center justify-center text-2xl">🏦</span>
        <div className="text-3xl font-semibold mt-4">{headline(o)}</div>
        <div className="text-sm mt-1">to {o.recipient.name}</div>
        <div className={`text-sm mt-1 ${FAILED.includes(o.stage) ? "text-red-600" : "text-slate-500"}`}>{STAGE_LABEL[o.stage]}</div>
      </div>

      <div className="flex rounded-lg bg-slate-100 p-1 mb-4 text-sm">
        {(["status", "detail"] as const).map((t) => (
          <button key={t} onClick={() => setTab(t)} className={`flex-1 h-8 rounded-md ${tab === t ? "bg-white shadow-sm" : "text-slate-500"}`}>
            {t === "status" ? "状态" : "详细信息"}
          </button>
        ))}
      </div>

      {tab === "status" ? (
        <Timeline o={o} />
      ) : (
        <>
          <p className="text-sm text-slate-500 mb-2">订单信息</p>
          <div className="rounded-xl bg-slate-50 px-4 py-1 mb-5">
            <KV k="UPB订单号" v={o.order_no} />
            <KV k="商户订单号" v={o.third_order_no} />
            <KV k="汇款用途" v={o.usage} />
            <KV k="汇款方式" v="SWIFT国际汇款" />
            <KV k="备注" v={o.note === o.usage ? "" : o.note} />
            <KV k="创建时间" v={fmtTime(o.created_at)} />
          </div>
          <p className="text-sm text-slate-500 mb-2">转账详情</p>
          <div className="rounded-xl bg-slate-50 px-4 py-1 mb-5">
            <KV k="汇出金额" v={`${o.amount} ${o.currency}`} />
            {q && (
              <>
                <KV k="支付金额" v={`${q.pay_amount} ${q.debit_coin}`} />
                <KV k="固定手续费" v={`${q.fixed_fee} ${q.fee_currency}`} />
                <KV k="交易手续费" v={`${q.transaction_fee} ${q.fee_currency}`} />
                <KV k="汇兑手续费" v={`${q.exchange_fee} ${q.fee_currency}`} />
                <KV k="付款总额" v={`${q.debit_total} ${q.debit_coin}`} />
              </>
            )}
            {o.exchange_rate && o.exchange_rate !== "0" && <KV k="执行汇率" v={o.exchange_rate} />}
          </div>
          <p className="text-sm text-slate-500 mb-2">收款人账户详情</p>
          <div className="rounded-xl bg-slate-50 px-4 py-1">
            <KV k="国家/地区" v={countryLabel(countries?.countries, o.recipient.bank_country)} />
            <KV k="名" v={o.recipient.given_name} />
            <KV k="姓" v={o.recipient.family_name} />
            <KV k="银行名称" v={o.recipient.bank_name} />
            <KV k="Swift" v={o.recipient.swift_code} />
            <KV k="银行账号" v={`**** ${o.recipient.account_last4}`} />
          </div>
        </>
      )}
    </PayoutLayout>
  );
}

// /payout/orders — history list.
export function OrderList() {
  const q = useInfiniteQuery({
    queryKey: ["payout-orders"],
    queryFn: ({ pageParam }) => payoutApi.orders(pageParam || undefined),
    initialPageParam: 0,
    getNextPageParam: (last) => (last.has_more ? last.orders[last.orders.length - 1].id : undefined),
  });
  const orders = q.data?.pages.flatMap((p) => p.orders) ?? [];
  return (
    <PayoutLayout back="/payout/recipients" close="/payout/recipients">
      <Title>汇款记录</Title>
      <ErrorBanner error={q.error} />
      {q.isLoading && <p className="text-sm text-slate-500">加载中…</p>}
      {!q.isLoading && orders.length === 0 && <p className="text-center text-sm text-slate-500 py-16">暂无汇款记录</p>}
      <div className="divide-y">
        {orders.map((o) => (
          <Link key={o.id} to={`/payout/orders/${o.id}`} className="flex items-center justify-between py-3">
            <span className="min-w-0">
              <span className="block truncate">{o.recipient_name || "收款人"}</span>
              <span className="block text-xs text-slate-400">{fmtTime(o.created_at)}</span>
            </span>
            <span className="text-right shrink-0 ml-3">
              <span className="block">
                {o.amount} {o.currency}
              </span>
              <span className={`block text-xs ${FAILED.includes(o.stage) ? "text-red-500" : "text-slate-500"}`}>
                {STAGE_LABEL[o.stage]}
              </span>
            </span>
          </Link>
        ))}
      </div>
      {q.hasNextPage && (
        <button onClick={() => q.fetchNextPage()} className="w-full text-sm text-emerald-600 py-4" disabled={q.isFetchingNextPage}>
          {q.isFetchingNextPage ? "加载中…" : "加载更多"}
        </button>
      )}
    </PayoutLayout>
  );
}

import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { apiClient, type CreateRefundReq } from "../api/client";

const statusLabel: Record<string, string> = {
  PENDING: "待支付",
  PAID: "已支付",
  EXPIRED: "已超时",
  CANCELED: "已取消",
  FAILED: "失败",
};

function shorten(id: string): string {
  return id.length <= 16 ? id : `${id.slice(0, 8)}…${id.slice(-8)}`;
}

function OrderId({ id }: { id: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard unavailable */
    }
  };
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="font-mono text-xs" title={id}>
        {shorten(id)}
      </span>
      <button
        onClick={copy}
        title="复制完整订单号"
        className="text-slate-400 hover:text-indigo-600 text-xs"
      >
        {copied ? "已复制" : "复制"}
      </button>
    </span>
  );
}

// ---- Refund dialog ----

const COINS = ["USDT", "USDC"] as const;
const CHAINS = ["TRON", "ETH", "BSC"] as const;

interface RefundDialogProps {
  orderId: string;
  amount: string;
  currency: string;
  onClose: () => void;
}

function RefundDialog({ orderId, amount, currency, onClose }: RefundDialogProps) {
  const qc = useQueryClient();
  const [form, setForm] = useState<CreateRefundReq>({
    coin: "USDT",
    chain: "TRON",
    to_address: "",
    reason: "",
  });
  const [error, setError] = useState("");

  const mut = useMutation({
    mutationFn: () => apiClient.createRefund(orderId, form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["orders"] });
      qc.invalidateQueries({ queryKey: ["refunds", orderId] });
      onClose();
    },
    onError: (e: Error) => setError(e.message),
  });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-md mx-4 p-6">
        <h2 className="text-lg font-semibold mb-1">申请退款</h2>
        <p className="text-sm text-slate-500 mb-4">
          退款金额：{amount} {currency}（全额退）
        </p>

        <div className="space-y-3">
          <div className="flex gap-3">
            <div className="flex-1">
              <label className="block text-xs text-slate-500 mb-1">币种</label>
              <select
                className="w-full border rounded-lg px-3 py-2 text-sm"
                value={form.coin}
                onChange={(e) => setForm({ ...form, coin: e.target.value })}
              >
                {COINS.map((c) => (
                  <option key={c}>{c}</option>
                ))}
              </select>
            </div>
            <div className="flex-1">
              <label className="block text-xs text-slate-500 mb-1">链</label>
              <select
                className="w-full border rounded-lg px-3 py-2 text-sm"
                value={form.chain}
                onChange={(e) => setForm({ ...form, chain: e.target.value })}
              >
                {CHAINS.map((c) => (
                  <option key={c}>{c}</option>
                ))}
              </select>
            </div>
          </div>

          <div>
            <label className="block text-xs text-slate-500 mb-1">收款钱包地址</label>
            <input
              className="w-full border rounded-lg px-3 py-2 text-sm font-mono"
              placeholder="0x… 或 T…"
              value={form.to_address}
              onChange={(e) => setForm({ ...form, to_address: e.target.value })}
            />
          </div>

          <div>
            <label className="block text-xs text-slate-500 mb-1">退款原因（选填）</label>
            <input
              className="w-full border rounded-lg px-3 py-2 text-sm"
              placeholder="可不填"
              value={form.reason}
              onChange={(e) => setForm({ ...form, reason: e.target.value })}
            />
          </div>
        </div>

        {error && <p className="mt-3 text-sm text-red-600">{error}</p>}

        <div className="mt-5 flex justify-end gap-2">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm rounded-lg border hover:bg-slate-50"
          >
            取消
          </button>
          <button
            onClick={() => mut.mutate()}
            disabled={mut.isPending || !form.to_address}
            className="px-4 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 disabled:opacity-50"
          >
            {mut.isPending ? "提交中…" : "提交退款"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---- Orders page ----

export default function Orders() {
  const { data: orders, isLoading } = useQuery({ queryKey: ["orders"], queryFn: apiClient.orders });
  const [refundFor, setRefundFor] = useState<{ id: string; amount: string; currency: string } | null>(null);

  if (isLoading) return <p>加载中…</p>;

  return (
    <div>
      <h1 className="text-xl font-bold mb-4">支付订单</h1>

      {refundFor && (
        <RefundDialog
          orderId={refundFor.id}
          amount={refundFor.amount}
          currency={refundFor.currency}
          onClose={() => setRefundFor(null)}
        />
      )}

      <div className="bg-white border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-500">
            <tr>
              <th className="text-left px-4 py-2">订单号</th>
              <th className="text-left px-4 py-2">周期</th>
              <th className="text-left px-4 py-2">金额</th>
              <th className="text-left px-4 py-2">状态</th>
              <th className="text-left px-4 py-2">创建时间</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {orders?.map((o) => (
              <tr key={o.order_id} className="border-t">
                <td className="px-4 py-2"><OrderId id={o.order_id} /></td>
                <td className="px-4 py-2">{o.plan_code}</td>
                <td className="px-4 py-2">{o.amount} {o.currency}</td>
                <td className="px-4 py-2">{statusLabel[o.status] || o.status}</td>
                <td className="px-4 py-2">{new Date(o.created_at).toLocaleString()}</td>
                <td className="px-4 py-2 text-right space-x-3">
                  {o.status === "PENDING" && (
                    <Link to={`/checkout/${o.order_id}`} className="text-indigo-600">
                      继续支付
                    </Link>
                  )}
                  {o.status === "PAID" && (
                    <>
                      <button
                        onClick={() =>
                          setRefundFor({ id: o.order_id, amount: o.amount, currency: o.currency })
                        }
                        className="text-amber-600 hover:text-amber-700"
                      >
                        申请退款
                      </button>
                      <Link
                        to={`/account/orders/${o.order_id}/refunds`}
                        className="text-slate-400 hover:text-indigo-600"
                      >
                        退款记录
                      </Link>
                    </>
                  )}
                </td>
              </tr>
            ))}
            {!orders?.length && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-400">
                  暂无订单
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

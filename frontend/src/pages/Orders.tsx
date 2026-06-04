import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { apiClient } from "../api/client";

const statusLabel: Record<string, string> = {
  PENDING: "待支付",
  PAID: "已支付",
  EXPIRED: "已超时",
  CANCELED: "已取消",
  FAILED: "失败",
};

// shorten keeps the first/last 8 chars and elides the middle, so the order id
// stays recognizable while fitting the column.
function shorten(id: string): string {
  return id.length <= 16 ? id : `${id.slice(0, 8)}…${id.slice(-8)}`;
}

// OrderId renders the elided id with a click-to-copy button for the full value.
function OrderId({ id }: { id: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard unavailable (e.g. non-secure context) — ignore */
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

export default function Orders() {
  const { data: orders, isLoading } = useQuery({ queryKey: ["orders"], queryFn: apiClient.orders });

  if (isLoading) return <p>加载中…</p>;

  return (
    <div>
      <h1 className="text-xl font-bold mb-4">支付订单</h1>
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
                <td className="px-4 py-2 text-right">
                  {o.status === "PENDING" && (
                    <Link to={`/checkout/${o.order_id}`} className="text-indigo-600">
                      继续支付
                    </Link>
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

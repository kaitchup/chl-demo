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
                <td className="px-4 py-2 font-mono text-xs">{o.order_id.slice(0, 16)}…</td>
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

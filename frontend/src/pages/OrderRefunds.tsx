import { useParams, Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "../api/client";

const refundStatusLabel: Record<string, string> = {
  PENDING: "处理中",
  PROCESSING: "处理中",
  SUCCEEDED: "退款成功",
  COMPLETED: "退款成功",
  FAILED: "退款失败",
  CANCELED: "已取消",
};

function shorten(s: string): string {
  return s.length <= 20 ? s : `${s.slice(0, 10)}…${s.slice(-10)}`;
}

function StatusBadge({ status }: { status: string }) {
  const label = refundStatusLabel[status] || status;
  const cls =
    status === "SUCCEEDED" || status === "COMPLETED"
      ? "bg-green-100 text-green-700"
      : status === "FAILED" || status === "CANCELED"
      ? "bg-red-100 text-red-700"
      : "bg-amber-100 text-amber-700";
  return (
    <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium ${cls}`}>
      {label}
    </span>
  );
}

export default function OrderRefunds() {
  const { id } = useParams<{ id: string }>();
  const orderId = id!;

  const { data: order, isLoading: orderLoading } = useQuery({
    queryKey: ["order", orderId],
    queryFn: () => apiClient.order(orderId),
  });

  const { data: refunds, isLoading: refundsLoading } = useQuery({
    queryKey: ["refunds", orderId],
    queryFn: () => apiClient.listOrderRefunds(orderId),
    refetchInterval: 30_000,
  });

  if (orderLoading || refundsLoading) return <p>加载中…</p>;

  return (
    <div>
      <div className="flex items-center gap-2 mb-4">
        <Link to="/account/orders" className="text-slate-400 hover:text-indigo-600 text-sm">
          ← 返回订单
        </Link>
      </div>

      {order && (
        <div className="bg-white border rounded-lg p-4 mb-6 text-sm">
          <p className="text-slate-500 text-xs mb-1">订单号</p>
          <p className="font-mono text-xs mb-3">{order.order_id}</p>
          <div className="flex gap-6">
            <div>
              <p className="text-slate-500 text-xs">金额</p>
              <p className="font-medium">
                {order.amount} {order.currency}
              </p>
            </div>
            <div>
              <p className="text-slate-500 text-xs">周期</p>
              <p className="font-medium">{order.plan_code}</p>
            </div>
            <div>
              <p className="text-slate-500 text-xs">支付时间</p>
              <p className="font-medium">
                {order.paid_at ? new Date(order.paid_at).toLocaleString() : "—"}
              </p>
            </div>
          </div>
        </div>
      )}

      <h2 className="text-base font-semibold mb-3">退款记录</h2>

      {!refunds?.length ? (
        <div className="bg-white border rounded-lg px-4 py-10 text-center text-slate-400 text-sm">
          暂无退款记录
        </div>
      ) : (
        <div className="space-y-3">
          {refunds.map((rf) => (
            <div key={rf.refund_id} className="bg-white border rounded-lg p-4 text-sm">
              <div className="flex items-start justify-between gap-4">
                <div className="space-y-1.5 flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">
                      {rf.amount} {rf.coin}
                    </span>
                    <span className="text-slate-400 text-xs">·</span>
                    <span className="text-slate-500 text-xs">{rf.chain}</span>
                    <StatusBadge status={rf.status} />
                  </div>
                  <p className="text-slate-500 text-xs font-mono truncate" title={rf.to_address}>
                    收款地址：{shorten(rf.to_address)}
                  </p>
                  {rf.reason && (
                    <p className="text-slate-500 text-xs">原因：{rf.reason}</p>
                  )}
                  {rf.upay_refund_id && (
                    <p className="text-slate-400 text-xs font-mono">
                      UPay ID：{rf.upay_refund_id}
                    </p>
                  )}
                </div>
                <div className="text-right shrink-0">
                  <p className="text-slate-400 text-xs">
                    {new Date(rf.created_at).toLocaleString()}
                  </p>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

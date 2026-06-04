import { useQuery } from "@tanstack/react-query";
import { useParams, Link, useNavigate } from "react-router-dom";
import { apiClient } from "../api/client";

const TERMINAL = ["PAID", "EXPIRED", "FAILED", "CANCELED"];

const statusLabel: Record<string, string> = {
  PENDING: "等待支付",
  PAID: "支付成功",
  EXPIRED: "已超时",
  FAILED: "支付失败",
  CANCELED: "已取消",
};

export default function Checkout() {
  const { id } = useParams<{ id: string }>();
  const nav = useNavigate();

  const { data: order, isLoading } = useQuery({
    queryKey: ["order", id],
    queryFn: () => apiClient.order(id!),
    // Poll until the order reaches a terminal state.
    refetchInterval: (q) => (TERMINAL.includes(q.state.data?.status ?? "") ? false : 4000),
  });

  if (isLoading) return <p>加载中…</p>;
  if (!order) return <p>订单不存在</p>;

  // Placeholder mode: backend returned a checkout_url on our own origin.
  const isPlaceholder = !order.checkout_url || order.checkout_url.startsWith(location.origin);
  const isTerminal = TERMINAL.includes(order.status);

  return (
    <div className="max-w-lg mx-auto bg-white border rounded-lg p-6">
      <h1 className="text-xl font-bold mb-2">订单收银台</h1>
      <p className="text-sm text-slate-500 mb-4 font-mono">{order.order_id}</p>

      <div className="space-y-1 text-sm mb-4">
        <p>套餐：{order.plan_code}</p>
        <p>金额：{order.amount} {order.currency}（≈ {order.amount} USDT）</p>
        <p>
          状态：<span className={order.status === "PAID" ? "text-green-600 font-medium" : ""}>
            {statusLabel[order.status] || order.status}
          </span>
        </p>
        {order.expires_at && <p>有效期至：{new Date(order.expires_at).toLocaleString()}</p>}
      </div>

      {/* Terminal states */}
      {order.status === "PAID" && (
        <div className="bg-green-50 border border-green-200 text-green-800 text-sm rounded p-4 mb-4">
          支付成功，会员已开通！
          <button onClick={() => nav("/account/subscription")} className="ml-2 underline">
            查看订阅
          </button>
        </div>
      )}
      {(order.status === "EXPIRED" || order.status === "FAILED") && (
        <div className="bg-red-50 border border-red-200 text-red-800 text-sm rounded p-4 mb-4">
          订单{statusLabel[order.status]}，请
          <Link to="/membership" className="underline ml-1">重新下单</Link>。
        </div>
      )}

      {/* Pending: real hosted checkout vs placeholder */}
      {!isTerminal && isPlaceholder && (
        <div className="bg-amber-50 border border-amber-200 text-amber-800 text-sm rounded p-4 mb-4">
          占位收银台（后端未配置 UPay）。配置 <code>UPAY_API_KEY</code> 后这里会内嵌真实 USDT 收银台。
        </div>
      )}
      {!isTerminal && !isPlaceholder && (
        <div className="mb-4 space-y-3">
          <p className="text-sm text-slate-500">
            点击下方按钮前往 UPay 安全收银台完成 USDT 支付。支付完成后会自动跳回本页，会员将在数秒内开通。
          </p>
          {/* UPay 托管收银台不能 iframe 内嵌：其会话 Cookie 是 SameSite=Lax/Strict，
              在跨站 iframe（第三方上下文）里会被浏览器拒收，页面会空白。改为整页跳转，
              付款后 UPay 经 success_url 跳回 /checkout/:id/result，由轮询接管开通。 */}
          <a
            href={order.checkout_url!}
            className="block w-full text-center bg-indigo-600 hover:bg-indigo-700 text-white font-medium rounded py-3"
          >
            前往 UPay 收银台支付 →
          </a>
          <p className="text-xs text-slate-400">
            收银台为 UPay 托管页面，需在其自有域名下打开；本页会持续轮询订单状态。
          </p>
        </div>
      )}

      <Link to="/account/orders" className="text-indigo-600 text-sm">
        ← 返回订单列表
      </Link>
    </div>
  );
}

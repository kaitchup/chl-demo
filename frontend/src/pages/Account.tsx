import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { apiClient } from "../api/client";

export default function Account() {
  const { data, isLoading } = useQuery({ queryKey: ["me"], queryFn: apiClient.me });

  if (isLoading) return <p>加载中…</p>;
  const m = data?.member;
  const statusLabel = { active: "生效中", expired: "已过期", none: "未开通" }[m?.status || "none"];

  return (
    <div className="space-y-6">
      <div className="bg-white border rounded-lg p-6">
        <h1 className="text-xl font-bold mb-4">个人中心</h1>
        <p className="text-sm text-slate-500">邮箱</p>
        <p className="mb-3">{data?.email}</p>
        <div className="flex items-center gap-2">
          <span className={`px-2 py-1 rounded text-sm ${m?.status === "active" ? "bg-green-100 text-green-700" : "bg-slate-100 text-slate-600"}`}>
            {m?.status === "active" ? "Super 会员" : "普通用户"}
          </span>
          <span className="text-sm text-slate-500">· {statusLabel}</span>
        </div>
      </div>

      <div className="bg-white border rounded-lg p-6">
        <h2 className="font-semibold mb-3">当前订阅</h2>
        <p className="text-sm">
          状态：{statusLabel}
          {m?.expires_at && ` · 到期 ${new Date(m.expires_at).toLocaleString()}（剩 ${m.days_left} 天）`}
        </p>
        <div className="mt-4 flex gap-3">
          <Link to="/membership" className="bg-indigo-600 text-white rounded px-4 py-2 text-sm">
            {m?.status === "active" ? "续费" : "立即订阅"}
          </Link>
          <Link to="/account/orders" className="border rounded px-4 py-2 text-sm">
            我的订单
          </Link>
        </div>
      </div>
    </div>
  );
}

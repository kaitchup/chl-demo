import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { apiClient, Plan } from "../api/client";
import { useAuth } from "../auth/AuthContext";

export default function Membership() {
  const { data: plans, isLoading } = useQuery({ queryKey: ["plans"], queryFn: apiClient.plans });
  const { token } = useAuth();
  const nav = useNavigate();
  const [omitUid, setOmitUid] = useState(false);

  const subscribe = async (plan: Plan) => {
    if (!token) {
      nav("/login");
      return;
    }
    const { order_id } = await apiClient.createOrder(plan.code, !omitUid);
    nav(`/checkout/${order_id}`);
  };

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">Super 会员</h1>
      <p className="text-slate-500 mb-6">解锁全部语种学习资源（英语 · 日语 · 阿拉伯语 · 韩语 · 俄语…），无限制使用。</p>
      <label className="flex items-center gap-2 mb-4 text-sm text-slate-500 cursor-pointer select-none">
        <input
          type="checkbox"
          checked={omitUid}
          onChange={(e) => setOmitUid(e.target.checked)}
          className="accent-indigo-600"
        />
        测试场景：本单不携带 UID（以订单号为准，每单独立 KYC）
      </label>
      {isLoading && <p>加载中…</p>}
      <div className="grid md:grid-cols-3 gap-4">
        {plans?.map((p) => (
          <div key={p.code} className="bg-white border rounded-lg p-5 flex flex-col">
            <h2 className="font-semibold">{p.name}</h2>
            <div className="my-3">
              <span className="text-3xl font-bold">{p.amount}</span>
              <span className="text-slate-500"> {p.currency}</span>
              <span className="text-slate-400 text-sm"> ≈ {p.amount} USDT</span>
            </div>
            <p className="text-sm text-slate-500 mb-4">{p.duration_days} 天有效期</p>
            <button onClick={() => subscribe(p)} className="mt-auto bg-indigo-600 text-white rounded py-2">
              订阅
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

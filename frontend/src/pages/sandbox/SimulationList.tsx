import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { sandboxClient, Simulation } from "../../api/sandboxClient";

const STATUS_LABEL: Record<string, { label: string; cls: string }> = {
  SIMULATING:    { label: "模拟中",     cls: "bg-blue-100 text-blue-700" },
  AML_PENDING:   { label: "AML 审核中", cls: "bg-amber-100 text-amber-700" },
  AML_APPROVED:  { label: "AML 已通过", cls: "bg-teal-100 text-teal-700" },
  DONE:          { label: "完成",       cls: "bg-green-100 text-green-700" },
  FAILED:        { label: "失败",       cls: "bg-red-100 text-red-700" },
};

function StatusBadge({ status }: { status: string }) {
  const s = STATUS_LABEL[status] ?? { label: status, cls: "bg-slate-100 text-slate-600" };
  return (
    <span className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${s.cls}`}>
      {s.label}
    </span>
  );
}

export default function SimulationList() {
  const [sims, setSims] = useState<Simulation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    sandboxClient
      .listSimulations()
      .then(setSims)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  if (loading) return <p className="text-slate-500">加载中…</p>;
  if (error) return <p className="text-red-600">{error}</p>;

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl font-semibold">模拟单列表</h1>
        <Link
          to="/sandbox/simulations/new"
          className="bg-indigo-600 text-white text-sm px-4 py-2 rounded-lg hover:bg-indigo-700"
        >
          + 新建模拟
        </Link>
      </div>

      {sims.length === 0 ? (
        <div className="text-center py-16 text-slate-400">
          <p className="text-lg mb-2">暂无模拟单</p>
          <p className="text-sm">点击右上角"新建模拟"创建第一条</p>
        </div>
      ) : (
        <div className="bg-white rounded-xl border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="px-4 py-3 text-left">订单 ID</th>
                <th className="px-4 py-3 text-left">场景</th>
                <th className="px-4 py-3 text-left">金额</th>
                <th className="px-4 py-3 text-left">状态</th>
                <th className="px-4 py-3 text-left">创建时间</th>
                <th className="px-4 py-3"></th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {sims.map((s) => (
                <tr key={s.id} className="hover:bg-slate-50">
                  <td className="px-4 py-3 font-mono text-xs text-slate-600">
                    {s.payment_request_id}
                  </td>
                  <td className="px-4 py-3">{s.scenario}</td>
                  <td className="px-4 py-3">
                    {s.amount} {s.coin ?? ""}
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={s.status} />
                  </td>
                  <td className="px-4 py-3 text-slate-500">
                    {new Date(s.created_at).toLocaleString("zh-CN")}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Link
                      to={`/sandbox/simulations/${s.id}`}
                      className="text-indigo-600 hover:underline"
                    >
                      详情
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

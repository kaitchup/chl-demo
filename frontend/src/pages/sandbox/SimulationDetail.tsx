import { useEffect, useState, useRef } from "react";
import { useParams, Link } from "react-router-dom";
import { sandboxClient, Simulation, AMLTicket, WebhookDelivery } from "../../api/sandboxClient";

const TERMINAL = new Set(["DONE", "FAILED", "AML_APPROVED"]);

const STATUS_LABEL: Record<string, { label: string; cls: string }> = {
  SIMULATING:   { label: "模拟中",     cls: "bg-blue-100 text-blue-700" },
  AML_PENDING:  { label: "AML 审核中", cls: "bg-amber-100 text-amber-700" },
  AML_APPROVED: { label: "AML 已通过", cls: "bg-teal-100 text-teal-700" },
  DONE:         { label: "完成",       cls: "bg-green-100 text-green-700" },
  FAILED:       { label: "失败",       cls: "bg-red-100 text-red-700" },
};

function StatusBadge({ status }: { status: string }) {
  const s = STATUS_LABEL[status] ?? { label: status, cls: "bg-slate-100 text-slate-600" };
  return (
    <span className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${s.cls}`}>
      {s.label}
    </span>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex py-2 border-b last:border-0 text-sm">
      <span className="w-36 text-slate-500 shrink-0">{label}</span>
      <span className="text-slate-800 break-all">{value}</span>
    </div>
  );
}

export default function SimulationDetail() {
  const { id } = useParams<{ id: string }>();
  const [sim, setSim] = useState<Simulation | null>(null);
  const [error, setError] = useState("");
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  function load() {
    if (!id) return;
    sandboxClient
      .getSimulation(Number(id))
      .then((s) => {
        setSim(s);
        if (TERMINAL.has(s.status) && timerRef.current) {
          clearInterval(timerRef.current);
          timerRef.current = null;
        }
      })
      .catch((e) => setError(e.message));
  }

  useEffect(() => {
    load();
    timerRef.current = setInterval(load, 5000);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [id]);

  if (error) return <p className="text-red-600">{error}</p>;
  if (!sim) return <p className="text-slate-500">加载中…</p>;

  const isTerminal = TERMINAL.has(sim.status);

  return (
    <div className="space-y-6">
      {/* breadcrumb */}
      <div className="text-sm text-slate-500">
        <Link to="/sandbox/simulations" className="hover:underline text-indigo-600">
          模拟单列表
        </Link>
        {" / "}
        <span>#{sim.id}</span>
      </div>

      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">模拟单详情</h1>
        <StatusBadge status={sim.status} />
        {!isTerminal && (
          <span className="text-xs text-slate-400 animate-pulse">每 5 秒自动刷新</span>
        )}
      </div>

      {/* basic info */}
      <div className="bg-white rounded-xl border px-5 py-1">
        <Row label="Payment Request" value={<span className="font-mono text-xs">{sim.payment_request_id}</span>} />
        <Row label="场景" value={sim.scenario} />
        <Row label="金额" value={`${sim.amount} ${sim.coin ?? ""}`} />
        {sim.to_address && <Row label="充值地址" value={<span className="font-mono text-xs">{sim.to_address}</span>} />}
        <Row label="重试" value={`${sim.retry_count} / ${sim.max_retries}`} />
        <Row label="创建时间" value={new Date(sim.created_at).toLocaleString("zh-CN")} />
        <Row label="更新时间" value={new Date(sim.updated_at).toLocaleString("zh-CN")} />
        {sim.error_detail && <Row label="错误详情" value={<span className="text-red-600">{sim.error_detail}</span>} />}
      </div>

      {/* AML tickets */}
      {sim.scenario === "RISK" && (
        <section>
          <h2 className="font-medium text-slate-700 mb-3">AML 工单</h2>
          {!sim.aml_tickets || sim.aml_tickets.length === 0 ? (
            <p className="text-sm text-slate-400">
              {sim.status === "SIMULATING" ? "等待充值记录出现…" : "暂无 AML 工单"}
            </p>
          ) : (
            <div className="bg-white rounded-xl border overflow-hidden">
              <table className="w-full text-sm">
                <thead className="bg-slate-50 text-xs text-slate-500 uppercase">
                  <tr>
                    <th className="px-4 py-2 text-left">Inspection ID</th>
                    <th className="px-4 py-2 text-left">Ticket UUID</th>
                    <th className="px-4 py-2 text-left">状态</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {sim.aml_tickets.map((t: AMLTicket) => (
                    <tr key={t.inspection_id}>
                      <td className="px-4 py-2 font-mono text-xs text-slate-600">{t.inspection_id}</td>
                      <td className="px-4 py-2 font-mono text-xs text-slate-600">
                        {t.ticket_uuid ?? <span className="text-slate-300">待发现</span>}
                      </td>
                      <td className="px-4 py-2">
                        {t.approved_at ? (
                          <span className="text-green-600 text-xs">
                            已审批 {new Date(t.approved_at).toLocaleString("zh-CN")}
                          </span>
                        ) : (
                          <span className="text-amber-600 text-xs">待审批</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {/* webhook deliveries */}
      <section>
        <h2 className="font-medium text-slate-700 mb-3">Webhook 投递记录</h2>
        {!sim.webhooks || sim.webhooks.length === 0 ? (
          <p className="text-sm text-slate-400">暂无投递记录</p>
        ) : (
          <div className="bg-white rounded-xl border overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-slate-50 text-xs text-slate-500 uppercase">
                <tr>
                  <th className="px-4 py-2 text-left">事件类型</th>
                  <th className="px-4 py-2 text-left">状态</th>
                  <th className="px-4 py-2 text-left">HTTP</th>
                  <th className="px-4 py-2 text-left">重试</th>
                  <th className="px-4 py-2 text-left">投递时间</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {sim.webhooks.map((w: WebhookDelivery) => (
                  <tr key={w.id}>
                    <td className="px-4 py-2 font-mono text-xs">{w.event_type}</td>
                    <td className="px-4 py-2">
                      <span
                        className={`text-xs font-medium ${
                          w.status === "SUCCESS" ? "text-green-600" : "text-red-600"
                        }`}
                      >
                        {w.status}
                      </span>
                    </td>
                    <td className="px-4 py-2 text-slate-500">{w.http_status || "—"}</td>
                    <td className="px-4 py-2 text-slate-500">{w.attempts}</td>
                    <td className="px-4 py-2 text-slate-500">
                      {w.delivered_at
                        ? new Date(w.delivered_at).toLocaleString("zh-CN")
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { sandboxClient, SimEvent, CreateSimulationReq } from "../../api/sandboxClient";

type Scenario = "NORMAL" | "BATCH" | "OVERPAID" | "RISK";

function round8(n: number) {
  return Math.round(n * 1e8) / 1e8;
}

function buildEvents(scenario: Scenario, fields: Record<string, any>): SimEvent[] {
  if (scenario === "NORMAL") {
    return [{ amount: String(fields.amount), status: "COMPLETED", delay_ms: 0, note: "" }];
  }
  if (scenario === "BATCH") {
    const total = Number(fields.amount) || 0;
    const n = Math.max(2, Math.min(10, Number(fields.count) || 2));
    const interval = Math.max(0, Number(fields.intervalSec) || 0) * 1000;
    const each = round8(total / n);
    const amounts = Array.from({ length: n - 1 }, () => each);
    amounts.push(round8(total - each * (n - 1)));
    return amounts.map((a, i) => ({
      amount: String(a),
      status: "COMPLETED",
      delay_ms: i === 0 ? 0 : interval,
      note: "",
    }));
  }
  if (scenario === "OVERPAID") {
    const base = Number(fields.amount) || 0;
    const extra = Number(fields.extra) || 0;
    return [
      { amount: String(round8(base + extra)), status: "COMPLETED", delay_ms: 0, note: `超额 +${extra}` },
    ];
  }
  if (scenario === "RISK") {
    return [{ amount: String(fields.amount), status: "FROZEN", delay_ms: 0, note: "风险占用" }];
  }
  return [];
}

export default function SimulationNew() {
  const nav = useNavigate();
  const [scenario, setScenario] = useState<Scenario>("NORMAL");
  const [paymentRequestId, setPaymentRequestId] = useState("");
  const [amount, setAmount] = useState("20");
  const [coin, setCoin] = useState("USDT");
  const [toAddress, setToAddress] = useState("");
  const [count, setCount] = useState(2);
  const [intervalSec, setIntervalSec] = useState(0);
  const [extra, setExtra] = useState("1");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");

    if (!paymentRequestId.trim()) {
      setError("payment_request_id 必填");
      return;
    }
    if (scenario === "RISK" && !toAddress.trim()) {
      setError("RISK 场景需要填写充值地址 (to_address)");
      return;
    }

    const events = buildEvents(scenario, { amount, count, intervalSec, extra });
    if (events.length === 0) {
      setError("无法构建事件，请检查参数");
      return;
    }

    const req: CreateSimulationReq = {
      payment_request_id: paymentRequestId.trim(),
      scenario,
      amount,
      coin: coin || undefined,
      to_address: scenario === "RISK" ? toAddress.trim() : undefined,
      events,
    };

    setSubmitting(true);
    try {
      const sim = await sandboxClient.createSimulation(req);
      nav(`/sandbox/simulations/${sim.id}`);
    } catch (err: any) {
      setError(err.message ?? "提交失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="max-w-xl">
      <h1 className="text-xl font-semibold mb-6">新建模拟</h1>

      <form onSubmit={handleSubmit} className="bg-white rounded-xl border p-6 space-y-5">
        {/* payment_request_id */}
        <Field label="Payment Request ID">
          <input
            className="input"
            placeholder="pay_01..."
            value={paymentRequestId}
            onChange={(e) => setPaymentRequestId(e.target.value)}
          />
        </Field>

        {/* scenario tabs */}
        <div>
          <label className="block text-sm font-medium text-slate-700 mb-2">场景</label>
          <div className="flex rounded-lg border overflow-hidden text-sm">
            {(["NORMAL", "BATCH", "OVERPAID", "RISK"] as Scenario[]).map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setScenario(s)}
                className={`flex-1 py-2 font-medium transition-colors ${
                  scenario === s
                    ? "bg-indigo-600 text-white"
                    : "text-slate-600 hover:bg-slate-50"
                }`}
              >
                {s === "NORMAL" ? "正常" : s === "BATCH" ? "分批" : s === "OVERPAID" ? "超额" : "RISK"}
              </button>
            ))}
          </div>
        </div>

        {/* amount + coin */}
        <div className="flex gap-3">
          <Field label="金额" className="flex-1">
            <input
              className="input"
              type="number"
              min="0"
              step="0.000001"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
          </Field>
          <Field label="币种" className="w-28">
            <input className="input" value={coin} onChange={(e) => setCoin(e.target.value)} />
          </Field>
        </div>

        {/* BATCH extras */}
        {scenario === "BATCH" && (
          <div className="flex gap-3">
            <Field label="笔数 (2–10)" className="flex-1">
              <input
                className="input"
                type="number"
                min={2}
                max={10}
                value={count}
                onChange={(e) => setCount(Number(e.target.value))}
              />
            </Field>
            <Field label="间隔 (秒)" className="flex-1">
              <input
                className="input"
                type="number"
                min={0}
                value={intervalSec}
                onChange={(e) => setIntervalSec(Number(e.target.value))}
              />
            </Field>
          </div>
        )}

        {/* OVERPAID extra */}
        {scenario === "OVERPAID" && (
          <Field label="超额金额">
            <input
              className="input"
              type="number"
              min="0"
              step="0.000001"
              value={extra}
              onChange={(e) => setExtra(e.target.value)}
            />
          </Field>
        )}

        {/* RISK to_address */}
        {scenario === "RISK" && (
          <Field label="充值地址 (to_address)">
            <input
              className="input font-mono text-xs"
              placeholder="0x..."
              value={toAddress}
              onChange={(e) => setToAddress(e.target.value)}
            />
            <p className="text-xs text-slate-400 mt-1">
              收银台页面中展示给付款方的 USDT 转入地址，用于匹配 AML 工单
            </p>
          </Field>
        )}

        {/* preview */}
        <div className="rounded-lg bg-slate-50 border p-3 text-xs space-y-1">
          <p className="font-medium text-slate-600 mb-2">预览事件</p>
          {buildEvents(scenario, { amount, count, intervalSec, extra }).map((ev, i) => (
            <div key={i} className="flex gap-3 text-slate-500">
              <span className="font-mono text-slate-400">#{i + 1}</span>
              <span className="font-mono">{ev.amount} {coin}</span>
              <span className={ev.status === "FROZEN" ? "text-amber-600" : "text-green-600"}>
                {ev.status}
              </span>
              {ev.delay_ms > 0 && <span>延时 {ev.delay_ms / 1000}s</span>}
              {ev.note && <span className="text-slate-400">{ev.note}</span>}
            </div>
          ))}
        </div>

        {error && (
          <p className="text-sm text-red-600 bg-red-50 border border-red-200 rounded px-3 py-2">
            {error}
          </p>
        )}

        <div className="flex gap-3 pt-1">
          <button
            type="button"
            onClick={() => nav("/sandbox/simulations")}
            className="flex-1 border rounded-lg py-2.5 text-sm text-slate-600 hover:bg-slate-50"
          >
            取消
          </button>
          <button
            type="submit"
            disabled={submitting}
            className="flex-1 bg-indigo-600 text-white rounded-lg py-2.5 text-sm font-medium
                       hover:bg-indigo-700 disabled:opacity-50"
          >
            {submitting ? "提交中…" : "开始模拟"}
          </button>
        </div>
      </form>
    </div>
  );
}

function Field({
  label,
  children,
  className = "",
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={className}>
      <label className="block text-sm font-medium text-slate-700 mb-1">{label}</label>
      {children}
    </div>
  );
}

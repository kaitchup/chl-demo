import { ReactNode } from "react";
import { Link, useNavigate, Navigate } from "react-router-dom";
import { useSandboxAuth } from "../auth/SandboxAuthContext";

export default function SandboxLayout({ children }: { children: ReactNode }) {
  const { token, merchantName, merchantId, clearAuth } = useSandboxAuth();
  const nav = useNavigate();

  if (!token) return <Navigate to="/sandbox/login" replace />;

  return (
    <div className="min-h-screen bg-slate-50">
      <header className="bg-white border-b">
        <div className="max-w-5xl mx-auto px-4 h-14 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Link to="/sandbox/simulations" className="font-semibold text-indigo-600">
              沙盒测试台
            </Link>
            <span className="text-xs text-slate-400 border border-slate-200 rounded px-1.5 py-0.5">
              仅 dev/staging
            </span>
          </div>
          <div className="flex items-center gap-4 text-sm">
            <span className="text-slate-500">
              {merchantName ?? merchantId}
            </span>
            <Link to="/sandbox/simulations/new" className="text-indigo-600 hover:underline">
              + 新建模拟
            </Link>
            <button
              onClick={() => { clearAuth(); nav("/sandbox/login"); }}
              className="text-slate-400 hover:text-red-500"
            >
              退出
            </button>
          </div>
        </div>
      </header>
      <main className="max-w-5xl mx-auto px-4 py-8">{children}</main>
    </div>
  );
}

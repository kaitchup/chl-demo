import { useState } from "react";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import { useSandboxAuth } from "../../auth/SandboxAuthContext";

const MERCHANT_ADMIN_PROFILE =
  (import.meta.env.VITE_MERCHANT_ADMIN_BASE || "https://merchant.upay-test.com/api/v1") +
  "/auth/profile";

export default function SandboxLogin() {
  const { setAuth } = useSandboxAuth();
  const nav = useNavigate();
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    const t = token.trim();
    if (!t) return;

    setLoading(true);
    try {
      const resp = await axios.get(MERCHANT_ADMIN_PROFILE, {
        headers: { Authorization: `Bearer ${t}` },
      });
      const user = resp.data?.user;
      if (!user?.merchant_id) {
        setError("响应中没有 merchant_id，请检查 token 是否来自正确的商户后台");
        return;
      }
      setAuth(t, user.merchant_id, user.merchant_name ?? user.merchant_id);
      nav("/sandbox/simulations");
    } catch (err: any) {
      if (err.response?.status === 401) {
        setError("Token 无效或已过期，请重新从商户后台复制");
      } else {
        setError("验证失败：" + (err.message ?? "未知错误"));
      }
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen bg-slate-50 flex items-center justify-center px-4">
      <div className="w-full max-w-md bg-white rounded-xl border p-8 shadow-sm">
        <h1 className="text-xl font-semibold mb-1">沙盒测试台</h1>
        <p className="text-sm text-slate-500 mb-6">
          从商户后台（merchant-admin）的开发者工具中复制访问 Token，粘贴到下方。
        </p>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-700 mb-1">
              Merchant-Admin Token
            </label>
            <textarea
              className="w-full border rounded-lg px-3 py-2 text-xs font-mono resize-none h-28 focus:outline-none focus:ring-2 focus:ring-indigo-500"
              placeholder="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
              value={token}
              onChange={(e) => setToken(e.target.value)}
            />
          </div>

          {error && (
            <p className="text-sm text-red-600 bg-red-50 border border-red-200 rounded px-3 py-2">
              {error}
            </p>
          )}

          <button
            type="submit"
            disabled={loading || !token.trim()}
            className="w-full bg-indigo-600 text-white rounded-lg py-2.5 text-sm font-medium
                       hover:bg-indigo-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading ? "验证中…" : "进入沙盒"}
          </button>
        </form>

        <p className="text-xs text-slate-400 mt-4">
          Token 仅存储在当前浏览器 session，关闭标签页后自动清除。
        </p>
      </div>
    </div>
  );
}

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import sandboxApi from "../../api/sandboxClient";
import { useSandboxAuth } from "../../auth/SandboxAuthContext";

export default function SandboxLogin() {
  const { setAuth } = useSandboxAuth();
  const nav = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    if (!email.trim() || !password) return;

    setLoading(true);
    try {
      const resp = await sandboxApi.post("/sandbox/login", {
        email: email.trim(),
        password,
      });
      const data = resp.data?.data;
      if (!data?.session_key || !data?.merchant_id) {
        setError("登录响应异常，请重试");
        return;
      }
      setAuth(data.session_key, data.merchant_id, data.merchant_name ?? data.merchant_id);
      nav("/sandbox/simulations");
    } catch (err: any) {
      if (err.response?.status === 401) {
        setError("邮箱或密码错误");
      } else {
        setError("登录失败：" + (err.message ?? "未知错误"));
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
          使用商户后台（merchant-admin）账号登录。
        </p>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-700 mb-1">邮箱</label>
            <input
              type="email"
              className="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              placeholder="merchant@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="email"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-slate-700 mb-1">密码</label>
            <input
              type="password"
              className="w-full border rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              placeholder="••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
            />
          </div>

          {error && (
            <p className="text-sm text-red-600 bg-red-50 border border-red-200 rounded px-3 py-2">
              {error}
            </p>
          )}

          <button
            type="submit"
            disabled={loading || !email.trim() || !password}
            className="w-full bg-indigo-600 text-white rounded-lg py-2.5 text-sm font-medium
                       hover:bg-indigo-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading ? "登录中…" : "进入沙盒"}
          </button>
        </form>
      </div>
    </div>
  );
}

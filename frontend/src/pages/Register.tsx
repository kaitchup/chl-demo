import { useState } from "react";
import { useNavigate, Link } from "react-router-dom";
import { apiClient } from "../api/client";
import { useAuth } from "../auth/AuthContext";

export default function Register() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const { login } = useAuth();
  const nav = useNavigate();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      const { token } = await apiClient.register(email, password);
      login(token);
      nav("/account");
    } catch (e: any) {
      setErr(e.message || "注册失败");
    }
  };

  return (
    <div className="max-w-sm mx-auto bg-white p-6 rounded-lg border">
      <h1 className="text-lg font-semibold mb-4">注册</h1>
      <form onSubmit={submit} className="space-y-3">
        <input className="w-full border rounded px-3 py-2" placeholder="邮箱" value={email} onChange={(e) => setEmail(e.target.value)} />
        <input className="w-full border rounded px-3 py-2" type="password" placeholder="密码（至少 6 位）" value={password} onChange={(e) => setPassword(e.target.value)} />
        {err && <p className="text-red-600 text-sm">{err}</p>}
        <button className="w-full bg-indigo-600 text-white rounded py-2">注册</button>
      </form>
      <p className="text-sm text-slate-500 mt-3">
        已有账号？<Link to="/login" className="text-indigo-600">登录</Link>
      </p>
    </div>
  );
}

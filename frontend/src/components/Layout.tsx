import { ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";

export default function Layout({ children }: { children: ReactNode }) {
  const { token, logout } = useAuth();
  const nav = useNavigate();

  return (
    <div className="min-h-screen">
      <header className="bg-white border-b">
        <div className="max-w-4xl mx-auto px-4 h-14 flex items-center justify-between">
          <Link to="/" className="font-semibold text-indigo-600">
            语言学习平台
          </Link>
          <nav className="flex items-center gap-4 text-sm">
            <Link to="/membership" className="hover:text-indigo-600">
              会员订阅
            </Link>
            {token ? (
              <>
                <Link to="/account" className="hover:text-indigo-600">
                  个人中心
                </Link>
                <Link to="/account/orders" className="hover:text-indigo-600">
                  订单
                </Link>
                <button
                  onClick={() => {
                    logout();
                    nav("/login");
                  }}
                  className="text-slate-500 hover:text-red-600"
                >
                  退出
                </button>
              </>
            ) : (
              <Link to="/login" className="hover:text-indigo-600">
                登录
              </Link>
            )}
          </nav>
        </div>
      </header>
      <main className="max-w-4xl mx-auto px-4 py-8">{children}</main>
    </div>
  );
}

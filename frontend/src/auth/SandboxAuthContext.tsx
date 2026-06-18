import { createContext, useContext, useState, ReactNode } from "react";

interface SandboxAuthState {
  token: string | null;
  merchantId: string | null;
  merchantName: string | null;
  setAuth: (token: string, merchantId: string, merchantName: string) => void;
  clearAuth: () => void;
}

const SandboxAuthContext = createContext<SandboxAuthState>({
  token: null,
  merchantId: null,
  merchantName: null,
  setAuth: () => {},
  clearAuth: () => {},
});

const SESSION_KEY = "sandbox_token";
const SESSION_MID = "sandbox_merchant_id";
const SESSION_MNAME = "sandbox_merchant_name";

export function SandboxAuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() => sessionStorage.getItem(SESSION_KEY));
  const [merchantId, setMerchantId] = useState<string | null>(() => sessionStorage.getItem(SESSION_MID));
  const [merchantName, setMerchantName] = useState<string | null>(() => sessionStorage.getItem(SESSION_MNAME));

  const setAuth = (t: string, mid: string, mname: string) => {
    sessionStorage.setItem(SESSION_KEY, t);
    sessionStorage.setItem(SESSION_MID, mid);
    sessionStorage.setItem(SESSION_MNAME, mname);
    setToken(t);
    setMerchantId(mid);
    setMerchantName(mname);
  };

  const clearAuth = () => {
    sessionStorage.removeItem(SESSION_KEY);
    sessionStorage.removeItem(SESSION_MID);
    sessionStorage.removeItem(SESSION_MNAME);
    setToken(null);
    setMerchantId(null);
    setMerchantName(null);
  };

  return (
    <SandboxAuthContext.Provider value={{ token, merchantId, merchantName, setAuth, clearAuth }}>
      {children}
    </SandboxAuthContext.Provider>
  );
}

export const useSandboxAuth = () => useContext(SandboxAuthContext);

// Hook for components that require auth — redirects to /sandbox/login if missing.
// Usage: const { token } = useRequireSandboxAuth();
export function useSandboxToken(): string {
  const { token } = useSandboxAuth();
  if (!token) throw new Error("not authenticated"); // caught by SandboxLayout
  return token;
}

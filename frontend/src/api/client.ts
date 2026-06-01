import axios from "axios";

// Same-origin /api in production (served by Caddy); Vite proxies it in dev.
const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE || "/api",
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem("token");
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

api.interceptors.response.use(
  (resp) => resp,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem("token");
      if (location.pathname !== "/login") location.href = "/login";
    }
    return Promise.reject(error);
  }
);

// Unwrap the { data, error } envelope.
function unwrap<T>(payload: any): T {
  if (payload?.error) throw new Error(payload.error.message || payload.error.code);
  return payload.data as T;
}

export interface Plan {
  code: string;
  name: string;
  amount: string;
  currency: string;
  duration_days: number;
}

export interface Member {
  tier: string;
  status: "none" | "active" | "expired";
  expires_at: string | null;
  days_left: number;
}

export interface Order {
  order_id: string;
  plan_code: string;
  amount: string;
  currency: string;
  status: string;
  received_amount: string;
  checkout_url: string | null;
  created_at: string;
  paid_at: string | null;
  expires_at: string | null;
}

export const apiClient = {
  register: (email: string, password: string) =>
    api.post("/auth/register", { email, password }).then((r) => unwrap<{ token: string }>(r.data)),
  login: (email: string, password: string) =>
    api.post("/auth/login", { email, password }).then((r) => unwrap<{ token: string }>(r.data)),
  me: () => api.get("/me").then((r) => unwrap<{ email: string; member: Member }>(r.data)),
  plans: () => api.get("/plans").then((r) => unwrap<Plan[]>(r.data)),
  subscription: () => api.get("/subscription").then((r) => unwrap<Member>(r.data)),
  createOrder: (plan_code: string) =>
    api.post("/orders", { plan_code }).then((r) => unwrap<{ order_id: string; checkout_url: string }>(r.data)),
  orders: () => api.get("/orders").then((r) => unwrap<Order[]>(r.data)),
  order: (id: string) => api.get(`/orders/${id}`).then((r) => unwrap<Order>(r.data)),
};

export default api;

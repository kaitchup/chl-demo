import axios from "axios";

const sandboxApi = axios.create({
  baseURL: import.meta.env.VITE_API_BASE || "/api",
});

// Attach the merchant-admin JWT from sessionStorage on every request.
sandboxApi.interceptors.request.use((config) => {
  const token = sessionStorage.getItem("sandbox_token");
  if (token) config.headers["X-Merchant-Token"] = token;
  return config;
});

sandboxApi.interceptors.response.use(
  (r) => r,
  (error) => {
    if (error.response?.status === 401) {
      sessionStorage.removeItem("sandbox_token");
      sessionStorage.removeItem("sandbox_merchant_id");
      sessionStorage.removeItem("sandbox_merchant_name");
      if (!location.pathname.startsWith("/sandbox/login")) {
        location.href = "/sandbox/login";
      }
    }
    return Promise.reject(error);
  }
);

function unwrap<T>(payload: any): T {
  if (payload?.error) throw new Error(payload.error.message || payload.error.code);
  return payload.data as T;
}

// ---- types ----

export interface AMLTicket {
  inspection_id: string;
  ticket_uuid: string | null;
  approved_at: string | null;
}

export interface WebhookDelivery {
  id: string;
  event_type: string;
  status: string;
  last_status_code: number;
  attempts: number;
  delivered_at_unix_micro: number | string | null;
  created_at_unix_micro: number | string;
}

export interface Simulation {
  id: number;
  payment_request_id: string;
  scenario: string;
  amount: string;
  coin: string | null;
  to_address: string | null;
  status: string;
  retry_count: number;
  max_retries: number;
  error_detail: string | null;
  created_at: string;
  updated_at: string;
  aml_tickets?: AMLTicket[];
  webhooks?: WebhookDelivery[];
}

export interface SimEvent {
  amount: string;
  status: string;
  delay_ms: number;
  note: string;
}

export interface CreateSimulationReq {
  payment_request_id: string;
  scenario: string;
  to_address?: string;
  amount: string;
  coin?: string;
  events: SimEvent[];
}

// ---- API ----

export const sandboxClient = {
  createSimulation: (req: CreateSimulationReq) =>
    sandboxApi.post("/sandbox/simulations", req).then((r) => unwrap<Simulation>(r.data)),

  listSimulations: (limit = 20, offset = 0) =>
    sandboxApi
      .get("/sandbox/simulations", { params: { limit, offset } })
      .then((r) => unwrap<Simulation[]>(r.data)),

  getSimulation: (id: number) =>
    sandboxApi.get(`/sandbox/simulations/${id}`).then((r) => unwrap<Simulation>(r.data)),
};

export default sandboxApi;

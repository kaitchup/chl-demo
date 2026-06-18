import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import App from "./App";
import { AuthProvider } from "./auth/AuthContext";
import { SandboxAuthProvider } from "./auth/SandboxAuthContext";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <SandboxAuthProvider>
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </SandboxAuthProvider>
      </AuthProvider>
    </QueryClientProvider>
  </React.StrictMode>
);

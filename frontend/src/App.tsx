import { Routes, Route, Navigate } from "react-router-dom";
import Layout from "./components/Layout";
import SandboxLayout from "./components/SandboxLayout";
import RequireAuth from "./components/RequireAuth";
import Login from "./pages/Login";
import Register from "./pages/Register";
import Membership from "./pages/Membership";
import Account from "./pages/Account";
import Orders from "./pages/Orders";
import OrderRefunds from "./pages/OrderRefunds";
import Checkout from "./pages/Checkout";
import SandboxLogin from "./pages/sandbox/SandboxLogin";
import SimulationList from "./pages/sandbox/SimulationList";
import SimulationNew from "./pages/sandbox/SimulationNew";
import SimulationDetail from "./pages/sandbox/SimulationDetail";

export default function App() {
  return (
    <Routes>
      {/* Sandbox — no consumer Layout */}
      <Route path="/sandbox/login" element={<SandboxLogin />} />
      <Route path="/sandbox/*" element={<SandboxRoutes />} />

      {/* Consumer app — wrapped in Layout */}
      <Route path="*" element={<ConsumerRoutes />} />
    </Routes>
  );
}

function SandboxRoutes() {
  return (
    <SandboxLayout>
      <Routes>
        <Route index element={<Navigate to="simulations" replace />} />
        <Route path="simulations" element={<SimulationList />} />
        <Route path="simulations/new" element={<SimulationNew />} />
        <Route path="simulations/:id" element={<SimulationDetail />} />
      </Routes>
    </SandboxLayout>
  );
}

function ConsumerRoutes() {
  return (
    <Layout>
      <Routes>
        <Route path="/" element={<Navigate to="/membership" replace />} />
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
        <Route path="/membership" element={<Membership />} />
        <Route
          path="/account"
          element={<RequireAuth><Account /></RequireAuth>}
        />
        <Route
          path="/account/subscription"
          element={<RequireAuth><Account /></RequireAuth>}
        />
        <Route
          path="/account/orders"
          element={<RequireAuth><Orders /></RequireAuth>}
        />
        <Route
          path="/account/orders/:id/refunds"
          element={<RequireAuth><OrderRefunds /></RequireAuth>}
        />
        <Route
          path="/checkout/:id"
          element={<RequireAuth><Checkout /></RequireAuth>}
        />
        <Route
          path="/checkout/:id/result"
          element={<RequireAuth><Checkout /></RequireAuth>}
        />
        <Route path="*" element={<Navigate to="/membership" replace />} />
      </Routes>
    </Layout>
  );
}

import { Navigate, Route, Routes } from "react-router-dom";
import { KycDone, PayoutEntry } from "./Entry";
import Kyc from "./Kyc";
import { RecipientNew, Recipients } from "./Recipients";
import Amount from "./Amount";
import Confirm from "./Confirm";
import { OrderDetail, OrderList } from "./Orders";

// Mounted at /payout/* outside the desktop Layout (mobile-first pages).
export default function PayoutRoutes() {
  return (
    <Routes>
      <Route index element={<PayoutEntry />} />
      <Route path="kyc" element={<Kyc />} />
      <Route path="kyc/done" element={<KycDone />} />
      <Route path="recipients" element={<Recipients />} />
      <Route path="recipients/new" element={<RecipientNew />} />
      <Route path="new" element={<Amount />} />
      <Route path="orders" element={<OrderList />} />
      <Route path="orders/:id" element={<OrderDetail />} />
      <Route path="orders/:id/confirm" element={<Confirm />} />
      <Route path="*" element={<Navigate to="/payout" replace />} />
    </Routes>
  );
}

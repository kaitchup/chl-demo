import { useQuery } from "@tanstack/react-query";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { payoutApi } from "../../api/payout";
import { Button, ErrorBanner, PayoutLayout } from "../../components/payout/ui";

export function useBootstrap() {
  return useQuery({ queryKey: ["payout-bootstrap"], queryFn: payoutApi.bootstrap });
}

// /payout: send users without a payer through KYC, everyone else to recipients.
export function PayoutEntry() {
  const { data, error, isLoading } = useBootstrap();
  if (isLoading) return <PayoutLayout back="/">加载中…</PayoutLayout>;
  if (error || !data)
    return (
      <PayoutLayout back="/">
        <ErrorBanner error={error} />
      </PayoutLayout>
    );
  return <Navigate to={data.has_payer ? "/payout/recipients" : "/payout/kyc"} replace />;
}

export function KycDone() {
  const nav = useNavigate();
  return (
    <PayoutLayout back={false} close="/payout" footer={<Button onClick={() => nav("/payout/recipients", { replace: true })}>开始汇款</Button>}>
      <h1 className="text-xl text-center mt-8">资料已提交</h1>
      <div className="flex justify-center my-16">
        <span className="w-40 h-40 rounded-full border-[10px] border-emerald-500 flex items-center justify-center text-7xl text-emerald-500">
          ✓
        </span>
      </div>
      <p className="text-center text-sm text-slate-500">
        付款人资料已提交至汇款服务。如需补充 KYC 资料，会在获取报价时提示。
      </p>
      <p className="text-center text-sm mt-6">
        <Link to="/payout/orders" className="text-emerald-600">
          查看汇款记录
        </Link>
      </p>
    </PayoutLayout>
  );
}

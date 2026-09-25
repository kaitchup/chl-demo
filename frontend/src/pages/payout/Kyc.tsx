import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
  DOC_TYPE_LABEL,
  EMPLOYMENT_LABEL,
  GENDER_LABEL,
  INCOME_LABEL,
  PayerInput,
  PayoutApiError,
  payoutApi,
} from "../../api/payout";
import {
  Button,
  CountrySelect,
  ErrorBanner,
  Field,
  ImageUpload,
  PayoutLayout,
  Select,
  TextInput,
  Title,
  Uploaded,
  useOptions,
} from "../../components/payout/ui";

// Field order follows the six steps; used to jump back on server validation errors.
const STEP_FIELDS: (keyof PayerInput)[][] = [
  ["given_name", "family_name", "name_local", "date_of_birth", "nationality", "gender"],
  ["email", "calling_code", "phone"],
  ["doc_type", "doc_number", "doc_issuing_country", "doc_issued_date", "doc_expiry_type", "doc_expiry_date"],
  ["identity_file_id"],
  ["addr_country", "addr_province", "addr_city", "addr_line1", "addr_line2", "addr_postal_code", "address_proof_file_id"],
  ["employment_status", "source_of_income", "occupation"],
];
const TITLES = ["基本信息", "联系方式", "证件信息", "证件上传", "居住地址", "职业与资金来源"];

const empty: PayerInput = {
  given_name: "", family_name: "", name_local: "", date_of_birth: "", nationality: "", gender: "",
  email: "", calling_code: "", phone: "",
  doc_type: "", doc_number: "", doc_issuing_country: "", doc_issued_date: "", doc_expiry_type: "dated", doc_expiry_date: "",
  identity_file_id: "", identity_file_name: "",
  addr_country: "", addr_province: "", addr_city: "", addr_line1: "", addr_line2: "", addr_postal_code: "",
  address_proof_file_id: "", address_proof_file_name: "",
  employment_status: "", source_of_income: "", occupation: "",
};

const label = (m: Record<string, string>, list?: string[]) => (list ?? []).map((v) => ({ value: v, label: m[v] ?? v }));

export default function Kyc() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const { data: opts } = useOptions();
  const [step, setStep] = useState(0);
  const [f, setF] = useState<PayerInput>(empty);
  const [identity, setIdentity] = useState<Uploaded | null>(null);
  const [proof, setProof] = useState<Uploaded | null>(null);
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof PayerInput) => (v: string) => setF((s) => ({ ...s, [k]: v }));

  // Personal data lives only in memory; warn before losing it.
  useEffect(() => {
    const h = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", h);
    return () => window.removeEventListener("beforeunload", h);
  }, []);

  const filled = (...ks: (keyof PayerInput)[]) => ks.every((k) => f[k].trim() !== "");
  const canNext = [
    filled("given_name", "family_name", "date_of_birth", "nationality", "gender"),
    filled("email", "calling_code", "phone"),
    filled("doc_type", "doc_number", "doc_issued_date") && (f.doc_expiry_type === "indefinite" || filled("doc_expiry_date")),
    !!identity,
    filled("addr_country", "addr_province", "addr_city", "addr_line1", "addr_postal_code") && !!proof,
    true,
  ][step];

  const submit = async () => {
    setErr(null);
    setBusy(true);
    try {
      await payoutApi.createPayer({
        ...f,
        doc_issuing_country: f.doc_issuing_country || f.nationality,
        identity_file_id: identity!.file_id,
        identity_file_name: identity!.file_name,
        address_proof_file_id: proof!.file_id,
        address_proof_file_name: proof!.file_name,
      });
      await qc.invalidateQueries({ queryKey: ["payout-bootstrap"] });
      nav("/payout/kyc/done", { replace: true });
    } catch (e) {
      setErr(e);
      if (e instanceof PayoutApiError) {
        if (e.code === "FILE_EXPIRED") {
          setIdentity(null);
          setProof(null);
          setStep(3);
        } else if (e.code === "VALIDATION") {
          const i = STEP_FIELDS.findIndex((fs) => fs.includes(e.details.field));
          if (i >= 0) setStep(i);
        } else if (e.code === "PAYER_EXISTS") {
          nav("/payout", { replace: true });
        }
      }
    } finally {
      setBusy(false);
    }
  };

  const next = () => (step === 5 ? submit() : setStep(step + 1));

  return (
    <PayoutLayout
      back={step === 0 ? "/payout" : () => setStep(step - 1)}
      progress={{ step: step + 1, total: 6 }}
      footer={
        <>
          {step > 0 && (
            <Button variant="secondary" onClick={() => setStep(step - 1)}>
              上一步
            </Button>
          )}
          <Button onClick={next} disabled={!canNext || busy}>
            {step === 5 ? (busy ? "提交中…" : "提交") : "继续"}
          </Button>
        </>
      }
    >
      <Title>{step === 0 ? "进行认证" : TITLES[step]}</Title>
      <ErrorBanner error={err} />

      {step === 0 && (
        <>
          <div className="flex gap-3">
            <div className="flex-1">
              <Field label="名" required>
                <TextInput value={f.given_name} onChange={set("given_name")} placeholder="英文，如 San" />
              </Field>
            </div>
            <div className="flex-1">
              <Field label="姓" required>
                <TextInput value={f.family_name} onChange={set("family_name")} placeholder="英文，如 Zhang" />
              </Field>
            </div>
          </div>
          <p className="-mt-2 mb-4 text-xs text-slate-400">需与证件一致，提交后不可修改</p>
          <Field label="本地语言姓名" hint="选填，不填则使用英文姓名">
            <TextInput value={f.name_local} onChange={set("name_local")} placeholder="如 张三" />
          </Field>
          <Field label="出生日期" required>
            <TextInput type="date" value={f.date_of_birth} onChange={set("date_of_birth")} />
          </Field>
          <Field label="国籍" required>
            <CountrySelect value={f.nationality} onChange={set("nationality")} placeholder="请选择国籍" />
          </Field>
          <Field label="性别" required>
            <Select value={f.gender} onChange={set("gender")} options={label(GENDER_LABEL, opts?.genders)} />
          </Field>
        </>
      )}

      {step === 1 && (
        <>
          <Field label="电子邮箱" required>
            <TextInput type="email" inputMode="email" value={f.email} onChange={set("email")} placeholder="电子邮箱" />
          </Field>
          <div className="flex gap-3">
            <div className="w-28">
              <Field label="区号" required>
                <TextInput value={f.calling_code} onChange={set("calling_code")} placeholder="+971" inputMode="tel" />
              </Field>
            </div>
            <div className="flex-1">
              <Field label="手机号" required>
                <TextInput value={f.phone} onChange={set("phone")} placeholder="手机号" inputMode="tel" />
              </Field>
            </div>
          </div>
        </>
      )}

      {step === 2 && (
        <>
          <Field label="证件类型" required>
            <Select value={f.doc_type} onChange={set("doc_type")} options={label(DOC_TYPE_LABEL, opts?.doc_types)} placeholder="请选择证件类型" />
          </Field>
          <Field label="证件号码" required>
            <TextInput value={f.doc_number} onChange={set("doc_number")} placeholder="请输入证件号码" />
          </Field>
          <Field label="签发国家/地区" hint="默认与国籍相同">
            <CountrySelect value={f.doc_issuing_country || f.nationality} onChange={set("doc_issuing_country")} />
          </Field>
          <Field label="签发日期" required>
            <TextInput type="date" value={f.doc_issued_date} onChange={set("doc_issued_date")} />
          </Field>
          <Field label="有效期" required>
            <Select
              value={f.doc_expiry_type}
              onChange={set("doc_expiry_type")}
              options={[
                { value: "dated", label: "有截止日期" },
                { value: "indefinite", label: "长期有效" },
              ]}
            />
          </Field>
          {f.doc_expiry_type === "dated" && (
            <Field label="到期日期" required>
              <TextInput type="date" value={f.doc_expiry_date} onChange={set("doc_expiry_date")} />
            </Field>
          )}
        </>
      )}

      {step === 3 && (
        <>
          <Field label="证件照片" required>
            <div className="flex">
              <ImageUpload label="证件照片" value={identity} onChange={setIdentity} accept="image/jpeg,image/png,application/pdf" />
            </div>
          </Field>
          <ul className="text-xs text-slate-400 space-y-1 list-disc pl-4">
            <li>支持 JPG、JPEG、PNG 或 PDF，图片超过 4MB 会自动压缩。</li>
            <li>上传您所选证件的完整照片，所有细节清晰可见。</li>
            <li>请确保证件为原件并在有效期内。</li>
            <li>请将证件置于纯色背景下拍摄。</li>
          </ul>
        </>
      )}

      {step === 4 && (
        <>
          <Field label="居住国家/地区" required>
            <CountrySelect value={f.addr_country} onChange={set("addr_country")} placeholder="请选择居住国家" />
          </Field>
          <div className="flex gap-3">
            <div className="flex-1">
              <Field label="省/州" required>
                <TextInput value={f.addr_province} onChange={set("addr_province")} />
              </Field>
            </div>
            <div className="flex-1">
              <Field label="城市" required>
                <TextInput value={f.addr_city} onChange={set("addr_city")} />
              </Field>
            </div>
          </div>
          <Field label="详细地址" required>
            <TextInput value={f.addr_line1} onChange={set("addr_line1")} placeholder="街道、门牌号" maxLength={100} />
          </Field>
          <Field label="地址补充">
            <TextInput value={f.addr_line2} onChange={set("addr_line2")} placeholder="楼栋、单元（选填）" maxLength={100} />
          </Field>
          <Field label="邮编" required>
            <TextInput value={f.addr_postal_code} onChange={set("addr_postal_code")} maxLength={20} />
          </Field>
          <Field label="地址证明" required hint="水电燃气账单、银行对账单等，需显示姓名与地址">
            <div className="flex">
              <ImageUpload label="地址证明" value={proof} onChange={setProof} />
            </div>
          </Field>
        </>
      )}

      {step === 5 && (
        <>
          <Field label="您的职业状况是？">
            <Select
              value={f.employment_status}
              onChange={set("employment_status")}
              options={label(EMPLOYMENT_LABEL, opts?.employment_status)}
              placeholder="请选择您的职业状况（选填）"
            />
          </Field>
          <Field label="您的主要资金来源是？">
            <Select
              value={f.source_of_income}
              onChange={set("source_of_income")}
              options={label(INCOME_LABEL, opts?.sources_of_income)}
              placeholder="请选择（选填）"
            />
          </Field>
          <Field label="职业">
            <TextInput value={f.occupation} onChange={set("occupation")} placeholder="如 Software Engineer（选填）" maxLength={100} />
          </Field>
          <div className="rounded-lg bg-slate-100 text-sm text-slate-600 px-3 py-3">您的信息将被加密并根据当地法规安全存储。</div>
        </>
      )}
    </PayoutLayout>
  );
}

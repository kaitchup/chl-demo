import { ReactNode, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Country, payoutApi, errorText } from "../../api/payout";

// Mobile-first shell for /payout/* (design is 375px wide). Desktop just sees a
// centered phone-width column.
export function PayoutLayout({
  children,
  footer,
  back,
  close = "/payout",
  right,
  progress,
}: {
  children: ReactNode;
  footer?: ReactNode;
  back?: string | (() => void) | false;
  close?: string | false;
  right?: ReactNode;
  progress?: { step: number; total: number };
}) {
  const nav = useNavigate();
  const goBack = () => (typeof back === "function" ? back() : typeof back === "string" ? nav(back) : nav(-1));
  return (
    <div className="min-h-screen bg-slate-100">
      <div className="mx-auto max-w-md min-h-screen bg-white flex flex-col">
        <header className="h-14 px-4 flex items-center gap-3 shrink-0">
          {back !== false ? (
            <button aria-label="返回" onClick={goBack} className="text-xl w-6">
              ←
            </button>
          ) : (
            <span className="w-6" />
          )}
          <div className="flex-1 flex gap-1.5 justify-center">
            {progress &&
              Array.from({ length: progress.total }).map((_, i) => (
                <span key={i} className={`h-0.5 w-10 rounded ${i < progress.step ? "bg-slate-900" : "bg-slate-200"}`} />
              ))}
          </div>
          {right ??
            (close !== false ? (
              <button aria-label="关闭" onClick={() => nav(close)} className="text-xl w-6">
                ×
              </button>
            ) : (
              <span className="w-6" />
            ))}
        </header>
        <main className="flex-1 px-4 pb-6">{children}</main>
        {footer && <footer className="sticky bottom-0 bg-white px-4 pt-3 pb-6 flex gap-3">{footer}</footer>}
      </div>
    </div>
  );
}

export function Title({ children, extra }: { children: ReactNode; extra?: ReactNode }) {
  return (
    <div className="flex items-center justify-between mt-2 mb-6">
      <h1 className="text-2xl font-medium">{children}</h1>
      {extra}
    </div>
  );
}

export function Button({
  children,
  onClick,
  disabled,
  variant = "primary",
  type = "button",
}: {
  children: ReactNode;
  onClick?: () => void;
  disabled?: boolean;
  variant?: "primary" | "secondary";
  type?: "button" | "submit";
}) {
  const cls =
    variant === "primary"
      ? "bg-emerald-500 text-white disabled:bg-emerald-200"
      : "bg-slate-100 text-slate-800 disabled:text-slate-400";
  return (
    <button type={type} onClick={onClick} disabled={disabled} className={`flex-1 h-11 rounded-lg text-base ${cls}`}>
      {children}
    </button>
  );
}

export function ErrorBanner({ error }: { error: unknown }) {
  if (!error) return null;
  return <div className="mb-4 rounded-lg bg-red-50 text-red-700 text-sm px-3 py-2">{errorText(error)}</div>;
}

export function Field({
  label,
  required,
  hint,
  children,
}: {
  label: string;
  required?: boolean;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="block mb-4">
      <span className="block text-sm mb-1.5">
        {required && <span className="text-red-500 mr-0.5">*</span>}
        {label}
      </span>
      {children}
      {hint && <span className="block text-xs text-slate-400 mt-1">{hint}</span>}
    </label>
  );
}

const inputCls =
  "w-full h-11 border border-slate-200 rounded-lg px-3 text-sm bg-white focus:outline-none focus:border-emerald-500 placeholder:text-slate-400";

export function TextInput(props: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: string;
  maxLength?: number;
  inputMode?: "text" | "decimal" | "numeric" | "email" | "tel";
}) {
  return (
    <input
      className={inputCls}
      value={props.value}
      type={props.type ?? "text"}
      inputMode={props.inputMode}
      maxLength={props.maxLength}
      placeholder={props.placeholder ?? "请输入"}
      onChange={(e) => props.onChange(e.target.value)}
    />
  );
}

export function TextArea(props: { value: string; onChange: (v: string) => void; maxLength: number }) {
  return (
    <div>
      <textarea
        className={`${inputCls} h-20 py-2 resize-none`}
        value={props.value}
        maxLength={props.maxLength}
        onChange={(e) => props.onChange(e.target.value)}
      />
      <div className="text-right text-xs text-slate-400">
        {props.value.length}/{props.maxLength}
      </div>
    </div>
  );
}

export function Select({
  value,
  onChange,
  options,
  placeholder = "请选择",
}: {
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string }[];
  placeholder?: string;
}) {
  return (
    <select
      className={`${inputCls} appearance-none ${value ? "" : "text-slate-400"}`}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      <option value="" disabled>
        {placeholder}
      </option>
      {options.map((o) => (
        <option key={o.value} value={o.value} className="text-slate-900">
          {o.label}
        </option>
      ))}
    </select>
  );
}

export function useOptions() {
  return useQuery({ queryKey: ["payout-options"], queryFn: payoutApi.options, staleTime: 60 * 60 * 1000 });
}

export function countryLabel(countries: Country[] | undefined, code: string) {
  const c = countries?.find((x) => x.code === code);
  return c ? c.name_zh || c.name_en : code;
}

export function CountrySelect({
  value,
  onChange,
  placeholder = "请选择国家或地区",
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  const { data, isLoading } = useOptions();
  const options = (data?.countries ?? [])
    .map((c) => ({ value: c.code, label: `${c.name_zh || c.name_en} (${c.code})` }))
    .sort((a, b) => a.label.localeCompare(b.label, "zh"));
  return (
    <Select
      value={value}
      onChange={onChange}
      options={options}
      placeholder={isLoading ? "加载中…" : placeholder}
    />
  );
}

// ---- image upload ----

const MAX_BYTES = 4 * 1024 * 1024;

// Downscale images above 4 MB to JPEG so phone photos pass without surprises.
async function shrink(file: File): Promise<File> {
  if (file.size <= MAX_BYTES || !file.type.startsWith("image/")) return file;
  const bmp = await createImageBitmap(file);
  let scale = Math.min(1, 2400 / Math.max(bmp.width, bmp.height));
  for (let quality = 0.85; quality >= 0.5; quality -= 0.1) {
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(bmp.width * scale);
    canvas.height = Math.round(bmp.height * scale);
    canvas.getContext("2d")!.drawImage(bmp, 0, 0, canvas.width, canvas.height);
    const blob: Blob | null = await new Promise((res) => canvas.toBlob(res, "image/jpeg", quality));
    if (blob && blob.size <= MAX_BYTES) {
      return new File([blob], file.name.replace(/\.\w+$/, "") + ".jpg", { type: "image/jpeg" });
    }
    scale *= 0.8;
  }
  throw new Error("图片过大，请换一张小于 4MB 的图片");
}

export interface Uploaded {
  file_id: string;
  file_name: string;
  preview?: string;
}

export function ImageUpload({
  label,
  value,
  onChange,
  accept = "image/jpeg,image/png",
}: {
  label: string;
  value: Uploaded | null;
  onChange: (v: Uploaded | null) => void;
  accept?: string;
}) {
  const ref = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>(null);

  const pick = async (f: File | undefined) => {
    if (!f) return;
    setErr(null);
    setBusy(true);
    try {
      const small = await shrink(f);
      const res = await payoutApi.upload(small);
      onChange({ ...res, preview: small.type.startsWith("image/") ? URL.createObjectURL(small) : undefined });
    } catch (e) {
      setErr(e);
      onChange(null);
    } finally {
      setBusy(false);
      if (ref.current) ref.current.value = "";
    }
  };

  return (
    <div className="flex-1">
      <button
        type="button"
        onClick={() => ref.current?.click()}
        className="w-full aspect-[4/3] rounded-lg border border-dashed border-slate-300 bg-slate-50 flex items-center justify-center overflow-hidden"
      >
        {busy ? (
          <span className="text-sm text-slate-500">上传中…</span>
        ) : value?.preview ? (
          <img src={value.preview} alt={label} className="w-full h-full object-cover" />
        ) : value ? (
          <span className="text-sm text-emerald-600 px-2 truncate">✓ {value.file_name}</span>
        ) : (
          <span className="w-10 h-10 rounded-full bg-slate-900 text-white flex items-center justify-center">📷</span>
        )}
      </button>
      <p className="text-center text-sm text-slate-500 mt-2">{value ? `重新上传${label}` : `上传${label}`}</p>
      {err && <p className="text-center text-xs text-red-600 mt-1">{errorText(err)}</p>}
      <input ref={ref} type="file" accept={accept} hidden onChange={(e) => pick(e.target.files?.[0])} />
    </div>
  );
}

// ---- countdown ----

export function useCountdown(expiresAt: number | null) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!expiresAt) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [expiresAt]);
  if (!expiresAt) return 0;
  return Math.max(0, Math.floor((expiresAt - now) / 1000));
}

export function mmss(sec: number) {
  return `${String(Math.floor(sec / 60)).padStart(2, "0")}:${String(sec % 60).padStart(2, "0")}`;
}

// ---- trade password sheet ----

export function PinSheet({
  mode,
  onSubmit,
  onClose,
}: {
  mode: "verify" | "setup";
  onSubmit: (pin: string) => Promise<void>; // throw to show an error and reset
  onClose: () => void;
}) {
  const [pin, setPin] = useState("");
  const [first, setFirst] = useState<string | null>(null);
  const [err, setErr] = useState<string>("");
  const [busy, setBusy] = useState(false);

  const title = mode === "verify" ? "交易密码" : first ? "再次输入交易密码" : "设置 6 位交易密码";

  useEffect(() => {
    if (pin.length !== 6 || busy) return;
    if (mode === "setup" && first === null) {
      setFirst(pin);
      setPin("");
      return;
    }
    if (mode === "setup" && first !== pin) {
      setErr("两次输入不一致，请重新设置");
      setFirst(null);
      setPin("");
      return;
    }
    setBusy(true);
    onSubmit(pin)
      .catch((e) => {
        setErr(errorText(e));
        setPin("");
        if (mode === "setup") setFirst(null);
      })
      .finally(() => setBusy(false));
  }, [pin]); // eslint-disable-line react-hooks/exhaustive-deps

  const press = (k: string) => {
    if (busy) return;
    setErr("");
    if (k === "del") setPin((p) => p.slice(0, -1));
    else if (pin.length < 6) setPin((p) => p + k);
  };

  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-end justify-center" onClick={onClose}>
      <div className="w-full max-w-md bg-white rounded-t-2xl pb-6" onClick={(e) => e.stopPropagation()}>
        <div className="mx-auto mt-2 mb-4 h-1 w-10 rounded bg-slate-200" />
        <div className="px-4">
          <h2 className="text-lg font-medium mb-4">{title}</h2>
          <div className="flex justify-between gap-2 mb-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <span key={i} className="flex-1 h-12 border border-slate-200 rounded-md flex items-center justify-center text-xl">
                {i < pin.length ? "•" : ""}
              </span>
            ))}
          </div>
          <p className="h-5 text-sm text-red-600">{busy ? <span className="text-slate-500">处理中…</span> : err}</p>
        </div>
        <div className="grid grid-cols-3 mt-2">
          {["1", "2", "3", "4", "5", "6", "7", "8", "9", "", "0", "del"].map((k, i) => (
            <button
              key={i}
              disabled={!k}
              onClick={() => press(k)}
              className="h-14 text-xl active:bg-slate-100 disabled:active:bg-transparent"
            >
              {k === "del" ? "⌫" : k}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

export function BankAvatar() {
  return (
    <span className="relative w-10 h-10 rounded-full bg-slate-100 flex items-center justify-center text-lg shrink-0">
      🏦
      <span className="absolute -right-0.5 -bottom-0.5 w-4 h-4 rounded-full bg-emerald-500 text-white text-[10px] flex items-center justify-center">
        $
      </span>
    </span>
  );
}

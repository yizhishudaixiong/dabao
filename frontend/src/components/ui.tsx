import React, { type ReactNode, type SelectHTMLAttributes, type InputHTMLAttributes } from 'react'

/* ---------- Card ---------- */
export function Card({
  title,
  desc,
  children,
  className = '',
}: {
  title?: string
  desc?: string
  children: ReactNode
  className?: string
}) {
  return (
    <section className={`card p-6 ${className}`}>
      {title && (
        <header className="mb-5">
          <h2 className="text-base font-semibold text-ink">{title}</h2>
          {desc && <p className="mt-1 text-xs text-ink-faint leading-relaxed">{desc}</p>}
        </header>
      )}
      {children}
    </section>
  )
}

/* ---------- PathField：只读路径输入框 + 浏览按钮（程序信息/输出位置等处共用） ---------- */
export function PathField({
  label,
  placeholder,
  value,
  onPick,
}: {
  label?: string
  placeholder?: string
  value: string
  onPick: () => void
}) {
  return (
    <div>
      {label && <span className="label">{label}</span>}
      <div className="flex gap-2">
        <input className="field" placeholder={placeholder} value={value} readOnly />
        <Button variant="ghost" onClick={onPick} className="shrink-0">
          📁 浏览…
        </Button>
      </div>
    </div>
  )
}

/* ---------- Button ---------- */
export function Button({
  variant = 'ghost',
  size = 'md',
  className = '',
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'ghost' | 'danger'
  size?: 'md' | 'sm'
}) {
  const map = {
    primary: 'btn-primary',
    ghost: 'btn-ghost',
    danger: 'btn-danger',
  }
  return (
    <button
      className={`${map[variant]} ${size === 'sm' ? '!px-3 !py-1.5 !text-xs' : ''} ${className}`}
      {...rest}
    >
      {children}
    </button>
  )
}

/* ---------- Badge ---------- */
export function Badge({
  tone = 'faint',
  children,
}: {
  tone?: 'good' | 'warn' | 'bad' | 'accent' | 'faint'
  children: ReactNode
}) {
  const map: Record<string, string> = {
    good: 'bg-good/15 text-good',
    warn: 'bg-warn/15 text-warn',
    bad: 'bg-bad/15 text-bad',
    accent: 'bg-accent/15 text-accent-bright',
    faint: 'bg-surface-raised text-ink-soft',
  }
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[13px] font-medium ${map[tone]}`}>
      {children}
    </span>
  )
}

/* ---------- Toggle ---------- */
export function Toggle({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  hint?: string
}) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className="group flex w-full items-center justify-between gap-4 rounded-panel border border-line bg-base px-4 py-3 text-left transition-colors hover:border-ink-faint"
    >
      <span>
        <span className="block text-sm text-ink">{label}</span>
        {hint && <span className="mt-0.5 block text-xs text-ink-faint">{hint}</span>}
      </span>
      <span
        className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
          checked ? 'bg-gradient-to-r from-accent to-accent-bright' : 'bg-surface-raised'
        }`}
      >
        <span
          className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-all ${
            checked ? 'left-[22px]' : 'left-0.5'
          }`}
        />
      </span>
    </button>
  )
}

/* ---------- Segmented control ---------- */
export function Seg<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { value: T; label: string; hint?: string }[]
  value: T
  onChange: (v: T) => void
}) {
  return (
    <div className="seg">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={`seg-item ${value === o.value ? 'seg-item-on' : ''}`}
          title={o.hint}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

/* ---------- Text input ---------- */
export function TextInput({
  label,
  ...rest
}: InputHTMLAttributes<HTMLInputElement> & { label?: string }) {
  return (
    <div>
      {label && <span className="label">{label}</span>}
      <input className="field" {...rest} />
    </div>
  )
}

/* ---------- Select ---------- */
export function Select({
  label,
  children,
  className = '',
  ...rest
}: SelectHTMLAttributes<HTMLSelectElement> & { label?: string }) {
  return (
    <div>
      {label && <span className="label">{label}</span>}
      <select
        className={`field appearance-none bg-[url('data:image/svg+xml;charset=utf-8,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20width%3D%2212%22%20height%3D%2212%22%20viewBox%3D%220%200%2024%2024%22%20fill%3D%22none%22%20stroke%3D%22%238b94a6%22%20stroke-width%3D%222.5%22%20stroke-linecap%3D%22round%22%20stroke-linejoin%3D%22round%22%3E%3Cpath%20d%3D%22m6%209%206%206%206-6%22%2F%3E%3C%2Fsvg%3E')] bg-no-repeat bg-[right_10px_center] pr-9 ${className}`}
        {...rest}
      >
        {children}
      </select>
    </div>
  )
}

/* ---------- Row helper ---------- */
export function Row({ label, value, hint }: { label: string; value?: string; hint?: string }) {
  return (
    <div className="flex items-start justify-between gap-4 py-1.5">
      <span className="text-sm text-ink-soft">{label}</span>
      <span className="max-w-[60%] text-right text-sm text-ink">{value}</span>
      {hint && <span className="text-xs text-ink-faint">{hint}</span>}
    </div>
  )
}

/* ---------- Modal ---------- */
export function Modal({
  title,
  onClose,
  children,
  maxW = 'max-w-md',
}: {
  title?: string
  onClose: () => void
  children: ReactNode
  maxW?: string
}) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-6"
      onClick={onClose}
    >
      <div
        className={`card w-full ${maxW} max-h-[90vh] overflow-auto p-7 shadow-card`}
        onClick={(e) => e.stopPropagation()}
      >
        {title && <h2 className="mb-4 text-base font-semibold text-ink">{title}</h2>}
        {children}
      </div>
    </div>
  )
}

export const STEPS = ['选择环境', '程序信息', '打包模式', '依赖检查', '开始构建']

export default function StepNav({
  current,
  onSelect,
  disabled,
}: {
  current: number
  onSelect: (i: number) => void
  disabled: (i: number) => boolean
}) {
  return (
    <nav className="flex items-center justify-center gap-1 px-8 pt-6">
      {STEPS.map((label, i) => {
        const active = i === current
        const done = i < current
        const blocked = disabled(i)
        return (
          <div key={label} className="flex items-center">
            <button
              type="button"
              disabled={blocked}
              onClick={() => onSelect(i)}
              className={`group flex items-center gap-2.5 rounded-full px-4 py-2 transition-all ${
                active
                  ? 'bg-surface-raised shadow-card'
                  : blocked
                    ? 'cursor-not-allowed opacity-40'
                    : 'hover:bg-surface-soft'
              }`}
            >
              <span
                className={`flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold transition-all ${
                  active
                    ? 'bg-gradient-to-br from-accent to-accent-bright text-white shadow-glow'
                    : done
                      ? 'bg-good/20 text-good'
                      : 'bg-surface-raised text-ink-faint'
                }`}
              >
                {done ? '✓' : i + 1}
              </span>
              <span
                className={`text-sm transition-colors ${
                  active ? 'font-medium text-ink' : 'text-ink-soft'
                }`}
              >
                {label}
              </span>
            </button>
            {i < STEPS.length - 1 && <span className="mx-1 h-px w-6 bg-line" />}
          </div>
        )
      })}
    </nav>
  )
}

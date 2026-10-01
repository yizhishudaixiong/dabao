import { useEffect, useRef } from 'react'

export default function LogPanel({ logs, height = 'h-56' }: { logs: string[]; height?: string }) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight
  }, [logs])

  // 复制全部日志到剪贴板（用 Wails 原生剪贴板接口，兼容性最好）
  const copyAll = async () => {
    try {
      await window.runtime.ClipboardSetText(logs.join('\n'))
    } catch {
      /* 剪贴板不可用时静默忽略 */
    }
  }

  return (
    <div>
      <div className="mb-2 flex justify-end">
        <button
          type="button"
          className="rounded-field border border-line bg-surface-raised px-2.5 py-1 text-xs text-ink-soft transition-colors hover:border-ink-faint disabled:opacity-40"
          onClick={copyAll}
          disabled={logs.length === 0}
        >
          复制全部日志
        </button>
      </div>
      {/* select-text 覆盖全局 select-none，允许鼠标选中日志复制 */}
      <div
        ref={ref}
        className={`${height} select-text overflow-auto rounded-panel border border-line bg-base p-4 font-mono text-xs leading-relaxed`}
      >
        {logs.length === 0 ? (
          <span className="text-ink-faint">暂无日志输出…</span>
        ) : (
          logs.map((l, i) => {
            let cls = 'text-ink-soft'
            if (l.startsWith('[清理]')) cls = 'text-good'
            else if (l.startsWith('>')) cls = 'text-accent-bright'
            else if (/error|failed|traceback|exception/i.test(l)) cls = 'text-bad'
            return (
              <div key={i} className={`whitespace-pre-wrap break-all ${cls}`}>
                {l || ' '}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

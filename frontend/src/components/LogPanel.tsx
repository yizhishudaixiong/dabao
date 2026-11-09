import { useEffect, useRef } from 'react'

// maxRender 单次最多渲染的行数：
// 日志面板不必把几千行全塞进 DOM（构建日志常见数千行），只渲染最近一段，
// 既能看到最新输出，又避免每次更新重排上千个节点。
// 「复制全部日志」复制的仍是完整日志，不受此限制。
const DEFAULT_MAX_RENDER = 800

export default function LogPanel({
  logs,
  height = 'h-56',
  maxRender = DEFAULT_MAX_RENDER,
}: {
  logs: string[]
  height?: string
  maxRender?: number
}) {
  const ref = useRef<HTMLDivElement>(null)
  // 用户是否停留在底部：是则跟随滚动，否则保持当前位置（往上翻查日志时不被拽回底部）
  const atBottom = useRef(true)

  useEffect(() => {
    const el = ref.current
    if (!el || !atBottom.current) return
    el.scrollTop = el.scrollHeight
  }, [logs])

  const hidden = Math.max(0, logs.length - maxRender)
  const view = hidden > 0 ? logs.slice(hidden) : logs

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
      <div className="mb-2 flex items-center justify-between gap-3">
        <span className="text-[11px] text-ink-faint">
          {hidden > 0 ? `仅显示最近 ${maxRender} 行（复制包含全部已保留的 ${logs.length} 行）` : ''}
        </span>
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
        onScroll={(e) => {
          const el = e.currentTarget
          atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
        }}
        className={`${height} select-text overflow-auto rounded-panel border border-line bg-base p-4 font-mono text-xs leading-relaxed`}
      >
        {logs.length === 0 ? (
          <span className="text-ink-faint">暂无日志输出…</span>
        ) : (
          view.map((l, i) => {
            let cls = 'text-ink-soft'
            if (l.startsWith('[清理]')) cls = 'text-good'
            else if (l.startsWith('>')) cls = 'text-accent-bright'
            else if (/error|failed|traceback|exception/i.test(l)) cls = 'text-bad'
            // key 用真实下标，保证截断后节点身份稳定
            return (
              <div key={hidden + i} className={`whitespace-pre-wrap break-all ${cls}`}>
                {l || ' '}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

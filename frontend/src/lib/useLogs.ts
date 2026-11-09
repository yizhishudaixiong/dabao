import { useCallback, useEffect, useRef, useState } from 'react'

// 日志缓冲区：把"每来一行就 setState"合并成"每帧一次 setState"。
//
// 原写法 setLogs(prev => [...prev, line]) 有两个问题：
//  1. 每条日志都整体复制一次数组 —— 几千行日志就是 O(n²) 的拷贝；
//  2. 每条日志都触发一次整页重渲染（PyInstaller/Pip 输出动辄数千行），构建越到后面越卡。
// 这里把同一帧内的多行攒成一批再更新，并限制保留的最大行数，避免内存无限增长。
export function useLogBuffer(limit = 4000) {
  const [lines, setLines] = useState<string[]>([])
  const pending = useRef<string[]>([])
  const frame = useRef<number | null>(null)

  const flush = useCallback(() => {
    frame.current = null
    if (pending.current.length === 0) return
    const batch = pending.current
    pending.current = []
    setLines((prev) => {
      const next = prev.concat(batch)
      return next.length > limit ? next.slice(next.length - limit) : next
    })
  }, [limit])

  const push = useCallback(
    (line: string) => {
      pending.current.push(line)
      if (frame.current === null) {
        frame.current = window.requestAnimationFrame(flush)
      }
    },
    [flush],
  )

  const clear = useCallback(() => {
    pending.current = []
    if (frame.current !== null) {
      window.cancelAnimationFrame(frame.current)
      frame.current = null
    }
    setLines([])
  }, [])

  // 卸载时取消未执行的回调，避免对已卸载组件 setState
  useEffect(
    () => () => {
      if (frame.current !== null) window.cancelAnimationFrame(frame.current)
      frame.current = null
    },
    [],
  )

  return { lines, push, clear }
}

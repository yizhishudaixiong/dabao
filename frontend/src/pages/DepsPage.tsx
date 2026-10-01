import { useEffect, useState } from 'react'
import { Badge, Button, Card, Seg } from '../components/ui'
import LogPanel from '../components/LogPanel'
import type { BuildConfig, DepInfo, DepsResult } from '../types/models'

// 将秒数格式化为 mm:ss
function fmtElapsed(total: number): string {
  const m = Math.floor(total / 60)
  const sec = total % 60
  return `${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
}

// pip 下载源：界面选项 key -> 实际源地址（__custom__ 表示用户自定义）
const MIRROR_OPTIONS = [
  { value: 'aliyun', label: '阿里源' },
  { value: 'tsinghua', label: '清华源' },
  { value: '', label: '官方源' },
  { value: '__custom__', label: '自定义源' },
]
const MIRROR_URLS: Record<string, string> = {
  aliyun: 'https://mirrors.aliyun.com/pypi/simple/',
  tsinghua: 'https://pypi.tuna.tsinghua.edu.cn/simple',
}

export default function DepsPage({
  cfg,
  updateCfg,
  deps,
  setDeps,
  depsLogs,
  depsProgress,
  analysisLogs,
  setAnalysisLogs,
  installing,
  setInstalling,
  analyzing,
  setAnalyzing,
  onCancelDeps,
}: {
  cfg: BuildConfig
  updateCfg: (patch: Partial<BuildConfig>) => void
  deps: DepsResult | null
  setDeps: (d: DepsResult | null) => void
  depsLogs: string[]
  depsProgress: { percent: number; stage: string } | null
  analysisLogs: string[]
  setAnalysisLogs: (l: string[]) => void
  installing: boolean
  setInstalling: (v: boolean) => void
  analyzing: boolean
  setAnalyzing: (v: boolean) => void
  onCancelDeps: () => void
}) {
  const [checked, setChecked] = useState<Set<string>>(new Set())
  // 隐式依赖导入是否展开（默认收起，点击按钮才显示；已有已添加项时默认展开）
  const [showHidden, setShowHidden] = useState(cfg.hiddenImports.length > 0)
  // 排除模块折叠（默认收起，点击按钮才展开；已有已添加项时默认展开）
  const [showExclude, setShowExclude] = useState(cfg.excludeModules.length > 0)
  // 已用时计时（秒）
  const [elapsed, setElapsed] = useState(0)
  // 下载源：默认阿里源；__custom__ 时使用自定义输入
  const [mirror, setMirror] = useState('aliyun')
  const [customMirror, setCustomMirror] = useState('')
  const [msg, setMsg] = useState('')
  const [hiddenInput, setHiddenInput] = useState('')
  const [excludeInput, setExcludeInput] = useState('')

  const ready = !!cfg.entryScript && !!cfg.pythonPath
  // 是否需要 PyArmor：用户选了代码加密档
  const needPyarmor = cfg.encryptMode !== 'none'
  // 实际传给后端的源地址
  const effectiveMirror =
    mirror === '__custom__' ? customMirror.trim() : MIRROR_URLS[mirror] ?? ''

  // 安装期间：每秒走一次计时
  useEffect(() => {
    if (!installing) {
      setElapsed(0)
      return
    }
    const t = setInterval(() => setElapsed((e) => e + 1), 1000)
    return () => clearInterval(t)
  }, [installing])

  const analyze = async () => {
    setAnalyzing(true)
    setMsg('')
    setAnalysisLogs([])
    try {
      // 后端统一分析：入口脚本依赖 + 必需依赖（PyInstaller，以及选了加密时的 PyArmor）
      const r = await window.go.main.App.AnalyzeDeps(cfg.entryScript, cfg.pythonPath, cfg.encryptMode, effectiveMirror)
      setDeps(r)
      // 缺失项全部默认勾选（必需项不可取消，由界面 disabled 保证）
      setChecked(new Set(r.missing.map((m) => m.pkgName)))
      if (r.missing.length === 0) {
        setMsg(needPyarmor ? 'PyArmor 已就绪，全部依赖均满足' : '全部依赖均已满足（未选择代码加密，不涉及 PyArmor）')
      }
    } catch (e) {
      setMsg('分析失败: ' + String(e))
    } finally {
      setAnalyzing(false)
    }
  }

  // 添加一个手动补充的隐式依赖导入模块（动态导入/插件式加载，静态分析发现不了）
  const addHidden = () => {
    const name = hiddenInput.trim()
    if (!name) return
    if (!cfg.hiddenImports.some((h) => h.toLowerCase() === name.toLowerCase())) {
      updateCfg({ hiddenImports: [...cfg.hiddenImports, name] })
    }
    setHiddenInput('')
  }

  // 移除一个隐式依赖导入模块
  const removeHidden = (name: string) => {
    updateCfg({ hiddenImports: cfg.hiddenImports.filter((h) => h !== name) })
  }

  // 添加一个要排除的模块（打包时 --exclude-module，缩小体积）
  const addExclude = () => {
    const name = excludeInput.trim()
    if (!name) return
    if (!cfg.excludeModules.some((m) => m.toLowerCase() === name.toLowerCase())) {
      updateCfg({ excludeModules: [...cfg.excludeModules, name] })
    }
    setExcludeInput('')
  }

  // 移除一个要排除的模块
  const removeExclude = (name: string) => {
    updateCfg({ excludeModules: cfg.excludeModules.filter((m) => m !== name) })
  }

  const install = async () => {
    // 带 requirements.txt 版本约束的包按指定版本安装，避免装到不兼容的最新版
    const installPkgs = deps
      ? deps.missing
          .filter((m) => checked.has(m.pkgName))
          .map((m) => (m.version ? `${m.pkgName}${m.version}` : m.pkgName))
      : []
    // 必需依赖（PyInstaller / 加密时的 PyArmor）即使被误取消勾选也强制一并安装
    const requiredPkgs = deps ? deps.missing.filter((m) => m.required).map((m) => m.pkgName) : []
    const allPkgs = Array.from(new Set([...installPkgs, ...requiredPkgs]))
    if (allPkgs.length === 0) return
    setInstalling(true)
    setMsg('')
    try {
      await window.go.main.App.InstallDeps(cfg.pythonPath, allPkgs, effectiveMirror)
      setMsg('安装完成，正在重新分析…')
      const r = await window.go.main.App.AnalyzeDeps(cfg.entryScript, cfg.pythonPath, cfg.encryptMode, effectiveMirror)
      setDeps(r)
      setChecked(new Set(r.missing.map((m) => m.pkgName)))
      setMsg(r.missing.length === 0 ? '所有依赖已就绪，可以开始打包' : '仍有部分依赖缺失，请检查安装日志')
    } catch (e) {
      setMsg('安装失败: ' + String(e))
    } finally {
      setInstalling(false)
    }
  }

  const toggle = (pkg: string) => {
    setChecked((prev) => {
      const next = new Set(prev)
      if (next.has(pkg)) next.delete(pkg)
      else next.add(pkg)
      return next
    })
  }

  // 合并展示：缺失在前（可勾选安装），已安装在后面（绿色标记）
  const all: Array<DepInfo & { state: 'missing' | 'resolved' }> = [
    ...(deps?.missing ?? []).map((m) => ({ ...m, state: 'missing' as const })),
    ...(deps?.resolved ?? []).map((m) => ({ ...m, state: 'resolved' as const })),
  ]

  return (
    <div className="space-y-5">
      <Card
        title="自动分析依赖"
        desc="由 PyInstaller 官方分析引擎完成（与正式打包同一套：字节码扫描 + 上千官方钩子 + 递归依赖树），自动补全动态导入等隐性依赖；首次分析约 5~15 秒，大项目（torch 等）30~60 秒，代码未变时重复分析秒回"
      >
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="primary" onClick={analyze} disabled={analyzing || installing || !ready}>
            {analyzing ? '⏳ 分析中…' : '🔍 分析依赖'}
          </Button>
          {!ready && (
            <span className="text-xs text-warn">请先在「选择环境」和「程序信息」中完成配置</span>
          )}
          {deps && (
            <span className="text-xs text-ink-soft">
              共 {deps.resolved.length + deps.missing.length} 项：已安装 {deps.resolved.length} 项，缺失{' '}
              {deps.missing.length} 项
              {deps.analysisSeconds > 0 && (
                <span className="text-ink-faint">
                  {' '}
                  · 分析耗时 {deps.analysisSeconds} 秒{deps.isCached ? '（命中缓存）' : ''}
                </span>
              )}
            </span>
          )}
        </div>
        {/* 分析引擎实时日志：分析中展示进度与原理说明 */}
        {analyzing && (
          <div className="mt-3 rounded-panel border border-line bg-base p-3">
            <div className="flex items-center gap-2 text-sm text-ink">
              <span className="inline-block h-3 w-3 animate-spin rounded-full border-2 border-line border-t-accent" />
              正在执行 PyInstaller 官方分析引擎…
            </div>
            <p className="mt-2 text-xs leading-relaxed text-ink-faint">
              原理：分析引擎会编译并扫描你的代码字节码、递归展开整棵依赖树、执行各包官方钩子，
              所以比传统源码扫描更全（能发现动态导入的依赖）。前几秒为固定开销（引擎自身加载），与项目大小无关。
            </p>
            {analysisLogs.length > 0 && (
              <div className="mt-2 max-h-24 overflow-auto rounded-field bg-surface-raised p-2 font-mono text-[11px] leading-relaxed text-ink-soft">
                {analysisLogs.slice(-8).map((l, i) => (
                  <div key={i} className="truncate">
                    {l}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
        <div className="mt-4 flex items-center justify-between gap-3">
          <span className="text-sm text-ink">下载源</span>
          <Seg
            value={mirror}
            onChange={setMirror}
            options={MIRROR_OPTIONS.map((m) => ({ value: m.value, label: m.label }))}
          />
        </div>
        {mirror === '__custom__' && (
          <div className="mt-3 rounded-panel border border-line bg-base p-4">
            <input
              className="field"
              placeholder="输入完整的 pip 镜像地址，如 https://pypi.tuna.tsinghua.edu.cn/simple"
              value={customMirror}
              onChange={(e) => setCustomMirror(e.target.value)}
            />
            <p className="mt-2 text-xs leading-relaxed text-ink-faint">
              <span className="font-medium text-ink">使用说明：</span>
              自定义源需要填完整的地址（带 http:// 或 https://）。国内常用源：
              清华 https://pypi.tuna.tsinghua.edu.cn/simple 、阿里
              https://mirrors.aliyun.com/pypi/simple/ 、中科大
              https://pypi.mirrors.ustc.edu.cn/simple 、豆瓣
              https://pypi.douban.com/simple 。不确定时建议直接选上面的阿里源/清华源。
            </p>
          </div>
        )}
        {needPyarmor && (
          <p className="mt-3 rounded-panel border border-warn/30 bg-warn/15 px-3 py-2 text-xs text-ink-soft">
            你已选择代码加密，分析时将自动检查 PyArmor 的安装状态。
          </p>
        )}
        {msg && <p className="mt-3 text-xs text-ink-soft">{msg}</p>}
      </Card>

      {deps && (
        <Card
          title="依赖清单"
          desc="绿色为已安装，红色为缺失；勾选缺失项后一键安装。带「必需」标记的为打包必需组件，缺失时必须安装"
        >
          {all.length === 0 ? (
            <p className="rounded-panel border border-dashed border-line p-6 text-center text-xs text-ink-faint">
              未检测到第三方依赖
            </p>
          ) : (
            <ul className="space-y-1.5">
              {all.map((m) => (
                <li key={m.pkgName}>
                  <label
                    className={`flex cursor-pointer items-center gap-3 rounded-field border px-3 py-2 transition-colors ${
                      m.state === 'missing'
                        ? 'border-line bg-base hover:border-ink-faint'
                        : 'border-line/60 bg-base/60 opacity-75'
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={m.state === 'missing' && (m.required || checked.has(m.pkgName))}
                      disabled={m.state === 'resolved' || m.required}
                      onChange={() => toggle(m.pkgName)}
                      className="h-4 w-4 accent-accent"
                    />
                    <span className="flex-1 truncate text-sm text-ink">
                      {m.pkgName}
                      {m.required && <Badge tone="warn">必需</Badge>}
                      {m.installedVersion && <span className="text-accent-soft"> {m.installedVersion}</span>}
                      {m.version && <span className="text-ink-faint">（要求{m.version}）</span>}
                    </span>
                    <span className="max-w-[40%] truncate text-xs text-ink-faint" title={`import ${m.importName}`}>
                      import {m.importName}
                    </span>
                    {m.source && (
                      <span
                        className="max-w-[30%] truncate font-mono text-[11px] text-ink-faint"
                        title={`来源：${m.source}`}
                      >
                        {m.source}
                      </span>
                    )}
                    {m.state === 'missing' ? <Badge tone="bad">缺失</Badge> : <Badge tone="good">已安装</Badge>}
                  </label>
                </li>
              ))}
            </ul>
          )}

          {/* 间接依赖（PyInstaller 钩子补充）：随主包 pip 自动安装，无需单独处理 */}
          {(deps.hookExtra?.length ?? 0) > 0 && (
            <div className="mt-4">
              <p className="text-xs text-ink-soft">
                <span className="font-medium text-ink">间接依赖（{deps.hookExtra.length} 项）</span>
                ——由分析引擎的官方钩子自动补充，随主包 pip 安装时一并装好，无需单独处理：
              </p>
              <ul className="mt-2 flex flex-wrap gap-1.5">
                {deps.hookExtra.map((h) => (
                  <li
                    key={h.pkgName}
                    className="flex items-center gap-1 rounded-full border border-line/60 bg-base/60 px-2.5 py-1 text-xs text-ink-soft"
                  >
                    {h.pkgName}
                    {h.installedVersion && <span className="text-accent-soft">{h.installedVersion}</span>}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {deps.missing.length > 0 && (
            <div className="mt-4 flex justify-end">
              <Button variant="primary" onClick={install} disabled={installing || checked.size === 0}>
                {installing ? '⏳ 安装中…' : `⬇ 安装缺失依赖（${checked.size}）`}
              </Button>
            </div>
          )}
        </Card>
      )}

      {/* 隐式依赖导入 + 排除模块：两个折叠按钮放在同一行，默认收起点击展开 */}
      <div className="flex flex-wrap gap-2">
        <Button variant="ghost" onClick={() => setShowHidden((v) => !v)}>
          {showHidden ? '▾ 收起隐式依赖导入' : '🧩 隐式依赖导入'}
        </Button>
        <Button variant="ghost" onClick={() => setShowExclude((v) => !v)}>
          {showExclude ? '▾ 收起排除模块' : '🚫 排除模块'}
        </Button>
      </div>

      {showHidden && (
        <Card
          title="隐式依赖导入"
          desc="补充动态导入/插件式加载等静态分析无法发现的隐式依赖，打包时作为 --hidden-import 传给 PyInstaller"
        >
          <div className="flex gap-2">
            <input
              className="field flex-1"
              placeholder="输入模块名，如：win32api、scipy.optimize"
              value={hiddenInput}
              onChange={(e) => setHiddenInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') addHidden()
              }}
            />
            <Button variant="primary" onClick={addHidden} disabled={!hiddenInput.trim()}> ＋ 添加</Button>
          </div>
          {cfg.hiddenImports.length > 0 ? (
            <ul className="mt-3 flex flex-wrap gap-2">
              {cfg.hiddenImports.map((h) => (
                <li
                  key={h}
                  className="flex items-center gap-1.5 rounded-full border border-line bg-surface-raised px-3 py-1 text-xs text-ink"
                >
                  {h}
                  <button
                    type="button"
                    className="text-ink-faint transition-colors hover:text-danger"
                    onClick={() => removeHidden(h)}
                    aria-label={`移除 ${h}`}
                  >
                    ✕
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-3 text-xs text-ink-faint">
              暂无。仅当程序使用变量动态导入（如 importlib.import_module(变量)）或按名称加载插件时才需要补充。
            </p>
          )}
        </Card>
      )}

      {showExclude && (
        <Card
          title="排除模块"
          desc="排除你确信用不到的重型库（如 torch、matplotlib、pandas），打包时跳过它们以明显减小体积"
        >
          <div className="flex gap-2">
            <input
              className="field flex-1"
              placeholder="输入模块名，如：torch、matplotlib、pandas"
              value={excludeInput}
              onChange={(e) => setExcludeInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') addExclude()
              }}
            />
            <Button variant="primary" onClick={addExclude} disabled={!excludeInput.trim()}> ＋ 添加</Button>
          </div>
          {cfg.excludeModules.length > 0 ? (
            <ul className="mt-3 flex flex-wrap gap-2">
              {cfg.excludeModules.map((m) => (
                <li
                  key={m}
                  className="flex items-center gap-1.5 rounded-full border border-line bg-surface-raised px-3 py-1 text-xs text-ink"
                >
                  {m}
                  <button
                    type="button"
                    className="text-ink-faint transition-colors hover:text-danger"
                    onClick={() => removeExclude(m)}
                    aria-label={`移除 ${m}`}
                  >
                    ✕
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-3 text-xs text-ink-faint">
              暂无排除项。{' '}
              <span className="text-warn">只排除确认用不到的库，否则打包后的程序可能报「缺少模块」。</span>
            </p>
          )}
        </Card>
      )}

      {/* 依赖安装进度面板：默认显示；加大进度条（完成绿色 / 未完成灰色）+ 已用时 + 强制停止按钮 */}
      <Card title="依赖安装进度" desc="实时展示依赖安装进度，日志可在下方查看与复制">
        <div className="space-y-3">
          <div className="flex items-center justify-between text-sm">
            <span className="text-ink">
              {installing ? depsProgress?.stage || '准备中…' : '尚未开始'}
            </span>
            <span className="font-mono text-ink-soft">
              {installing
                ? `${depsProgress?.percent ?? 0}% · 已用时 ${fmtElapsed(elapsed)}`
                : '点击「安装缺失依赖」后自动更新'}
            </span>
          </div>
          <div className="h-4 w-full overflow-hidden rounded-full bg-line">
            <div
              className="progress-animated h-full rounded-full bg-good transition-all duration-300"
              style={{
                width: `${installing ? Math.min(100, Math.max(0, depsProgress?.percent ?? 0)) : 0}%`,
              }}
            />
          </div>
          <p className="truncate text-xs text-ink-faint">
            最近：{depsLogs.length > 0 ? depsLogs[depsLogs.length - 1] : '暂无日志'}
          </p>
          {installing && (
            <div className="flex justify-end">
              <Button variant="danger" onClick={onCancelDeps}>
                ⏹ 强制停止安装
              </Button>
            </div>
          )}
        </div>
      </Card>

      <Card title="安装日志">
        <LogPanel logs={depsLogs} />
      </Card>
    </div>
  )
}

import { Component, useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import StepNav, { STEPS } from './components/StepNav'
import { Button, Modal, Toggle } from './components/ui'
import { BrowserOpenURL, OnFileDrop, OnFileDropOff, Quit } from '../wailsjs/runtime/runtime'
import icon from './assets/icon.png'
import { APP_POST_URL, APP_REPO_URL } from './constants'
import { baseName, dirOf } from './lib/path'
import EnvPage from './pages/EnvPage'
import ConfigPage from './pages/ConfigPage'
import ModePage from './pages/ModePage'
import DepsPage from './pages/DepsPage'
import BuildPage from './pages/BuildPage'
import type { BuildConfig, BuildDonePayload, DepsResult, PythonEnv } from './types/models'

// 全局错误边界：任何页面渲染出错时显示中文错误页，避免白屏，方便排查
class ErrorBoundary extends Component<{ children: ReactNode }, { err: string | null }> {
  state = { err: null as string | null }
  static getDerivedStateFromError(e: unknown) {
    return { err: e instanceof Error ? e.message : String(e) }
  }
  render() {
    if (this.state.err) {
      return (
        <div className="flex h-full flex-col items-center justify-center gap-3 bg-base p-8 text-center">
          <p className="text-lg font-semibold text-ink">😵 页面遇到了一点问题</p>
          <p className="max-w-md text-xs leading-relaxed text-ink-faint">
            以下为错误信息（可截图反馈给作者）：<br />
            {this.state.err}
          </p>
          <Button variant="primary" size="sm" onClick={() => location.reload()}>
            🔄 重新加载
          </Button>
        </div>
      )
    }
    return this.props.children
  }
}

function defaultCfg(): BuildConfig {
  return {
    name: '',
    version: '',
    author: '',
    iconPath: '',
    entryScript: '',
    oneFile: true,
    noConsole: true,
    liteMode: false,
    encryptMode: 'none',
    resources: [],
    outputDir: '',
    contentsDir: '',
    hiddenImports: [],
    useUPX: false,
    excludeModules: [],
    pythonPath: '',
    pipMirror: '',
    extraArgs: [],
    cacheAccel: true,
    uacAdmin: false,
  }
}

export default function App() {
  const [step, setStep] = useState(0)
  const [cfg, setCfg] = useState<BuildConfig>(defaultCfg)
  const [envs, setEnvs] = useState<PythonEnv[]>([])
  const [selected, setSelected] = useState<PythonEnv | null>(null)

  const [deps, setDeps] = useState<DepsResult | null>(null)
  const [buildLogs, setBuildLogs] = useState<string[]>([])
  const [depsLogs, setDepsLogs] = useState<string[]>([])
  // 依赖安装进度（后端 deps:progress 事件推送）
  const [depsProgress, setDepsProgress] = useState<{ percent: number; stage: string } | null>(null)
  // 依赖分析引擎的实时日志（PyInstaller 官方分析）
  const [analysisLogs, setAnalysisLogs] = useState<string[]>([])
  const [done, setDone] = useState<BuildDonePayload | null>(null)
  // 程序内部版本号（后端 GetVersion 返回，唯一版本来源在 internal/version）
  const [appVersion, setAppVersion] = useState('')
  // 关于弹窗是否显示（点击右上角版本号打开）
  const [showAbout, setShowAbout] = useState(false)
  // 忙碌状态下退出确认框是否显示（后端通知弹出）
  const [showExitConfirm, setShowExitConfirm] = useState(false)
  // 轻量提示（拖拽结果 / 配置导入导出结果），3 秒自动消失
  const [toast, setToast] = useState<string | null>(null)
  const toastTimer = useRef<number | undefined>(undefined)
  const showToast = (msg: string) => {
    setToast(msg)
    window.clearTimeout(toastTimer.current)
    toastTimer.current = window.setTimeout(() => setToast(null), 3000)
  }

  // 监听后端「窗口关闭请求」事件：忙碌时弹出自定义确认框（代替系统原生弹窗）
  useEffect(() => {
    const off = window.runtime.EventsOn('app:exit-request', () => setShowExitConfirm(true))
    return () => off()
  }, [])

  // 用户点击「继续退出」：标记已确认后强制退出
  const confirmExitNow = async () => {
    try {
      await window.go.main.App.ConfirmExit()
    } catch {
      /* 忽略异常，继续尝试退出 */
    }
    Quit()
  }

  // 导出当前打包配置为 JSON 文件（不含环境路径，适合分享/备份）
  const saveConfig = async () => {
    try {
      const p = await window.go.main.App.SaveConfigToFile(cfg)
      if (p) showToast('配置已保存：' + p)
    } catch (e) {
      const msg = String(e)
      if (!msg.includes('已取消')) showToast('保存失败：' + msg)
    }
  }

  // 从 JSON 文件导入打包配置（保留当前已选环境）
  const loadConfig = async () => {
    try {
      const c = await window.go.main.App.LoadConfigFromFile()
      if (!c) return
      setCfg((prev) => ({
        ...c,
        pythonPath: prev.pythonPath,
        resources: Array.isArray(c.resources) ? c.resources : [],
        hiddenImports: Array.isArray(c.hiddenImports) ? c.hiddenImports : [],
        excludeModules: Array.isArray(c.excludeModules) ? c.excludeModules : [],
        extraArgs: Array.isArray(c.extraArgs) ? c.extraArgs : [],
      }))
      setShowAbout(false)
      showToast('配置已导入，可继续调整或直接打包')
    } catch (e) {
      const msg = String(e)
      if (!msg.includes('已取消')) showToast('导入失败：' + msg)
    }
  }
  // === 统一忙碌状态：打包/安装/依赖分析/资源分析/便携版下载 共用一套 ===
  // 任何长任务开始 markBusy('名称')、结束 unmarkBusy('名称')；
  // busy=true 时禁用步骤导航与上一步/下一步（后端窗口关闭拦截用同一套计数，两边始终一致）。
  const [busyTasks, setBusyTasks] = useState<Set<string>>(new Set())
  const markBusy = useCallback((name: string) => {
    setBusyTasks((prev) => new Set(prev).add(name))
  }, [])
  const unmarkBusy = useCallback((name: string) => {
    setBusyTasks((prev) => {
      const s = new Set(prev)
      s.delete(name)
      return s
    })
  }, [])
  const busy = busyTasks.size > 0
  const building = busyTasks.has('building')
  const installing = busyTasks.has('installing')
  const analyzing = busyTasks.has('analyzing')
  const resAnalyzing = busyTasks.has('resAnalyzing')
  const portableDl = busyTasks.has('portable')
  // setter 包装：页面继续用 setXxx(true/false) 的写法，内部自动走统一计数
  const setBuilding = (v: boolean) => (v ? markBusy('building') : unmarkBusy('building'))
  const setInstalling = (v: boolean) => (v ? markBusy('installing') : unmarkBusy('installing'))
  const setAnalyzing = (v: boolean) => (v ? markBusy('analyzing') : unmarkBusy('analyzing'))
  const setResAnalyzing = (v: boolean) => (v ? markBusy('resAnalyzing') : unmarkBusy('resAnalyzing'))
  const setDownloading = (v: boolean) => (v ? markBusy('portable') : unmarkBusy('portable'))

  // 打包完成声音提示开关（localStorage 持久化，实时生效）
  const [soundEnabled, setSoundEnabled] = useState<boolean>(() => {
    try {
      return localStorage.getItem('pp_sound') === '1'
    } catch {
      return false
    }
  })
  const soundRef = useRef(soundEnabled)
  useEffect(() => {
    soundRef.current = soundEnabled
  }, [soundEnabled])

  // 记忆功能开关（localStorage 持久化，默认关闭）：
  // 开启 = 自动保存全部配置到数据目录、下次打开自动还原；关闭 = 删除记忆文件、不再保存
  const [memoryEnabled, setMemoryEnabled] = useState<boolean>(() => {
    try {
      return localStorage.getItem('pp_memory') === '1'
    } catch {
      return false
    }
  })

  // 切换记忆功能：开启立刻保存当前配置，关闭立刻删除记忆文件
  const toggleMemory = (v: boolean) => {
    setMemoryEnabled(v)
    try {
      localStorage.setItem('pp_memory', v ? '1' : '0')
    } catch {
      /* 忽略存储失败 */
    }
    if (v) {
      window.go.main.App.SaveAutoConfig(cfg)
        .then(() => showToast('已开启记忆，当前配置已自动保存'))
        .catch(() => showToast('开启记忆失败'))
    } else {
      window.go.main.App.DeleteAutoConfig()
        .then(() => showToast('已关闭记忆，记忆文件已删除'))
        .catch(() => showToast('删除记忆文件失败'))
    }
  }

  const toggleSound = (v: boolean) => {
    setSoundEnabled(v)
    try {
      localStorage.setItem('pp_sound', v ? '1' : '0')
    } catch {
      /* 忽略存储失败 */
    }
  }

  // 打包完成提示音：用 Web Audio 合成，无需任何音频文件
  // 成功 = 上行三音（C5-E5-G5，欢快），失败 = 下行三音（E4-C4-A3，低沉）
  // 每个音 0.7 秒、末音拖长到 1.4 秒，总时长约 2.4~2.5 秒，不容易漏听
  function playDoneSound(success: boolean) {
    try {
      type AC = typeof window.AudioContext
      const Ctx = window.AudioContext || (window as unknown as { webkitAudioContext: AC }).webkitAudioContext
      if (!Ctx) return
      const ctx = new Ctx()
      const tone = (freq: number, delay: number, dur: number, vol = 0.25) => {
        const osc = ctx.createOscillator()
        const gain = ctx.createGain()
        osc.type = 'sine'
        osc.frequency.value = freq
        osc.connect(gain)
        gain.connect(ctx.destination)
        const t = ctx.currentTime + delay
        gain.gain.setValueAtTime(0.0001, t)
        gain.gain.exponentialRampToValueAtTime(vol, t + 0.02)
        gain.gain.exponentialRampToValueAtTime(0.0001, t + dur)
        osc.start(t)
        osc.stop(t + dur)
      }
      const notes = success ? [523.25, 659.25, 783.99] : [329.63, 261.63, 220]
      notes.forEach((f, i) => {
        const last = i === notes.length - 1
        tone(f, i * 0.28, last ? 1.4 : 0.7, success ? 0.26 : 0.3)
      })
      // 等声音完整播完再关闭音频上下文（约 3 秒），避免被截断
      setTimeout(() => {
        try {
          ctx.close()
        } catch {
          /* 忽略 */
        }
      }, 3500)
    } catch {
      /* 声音失败不影响功能 */
    }
  }

  const updateCfg = (patch: Partial<BuildConfig>) => setCfg((c) => ({ ...c, ...patch }))

  // 启动时读取程序内部版本号并显示在右上角
  useEffect(() => {
    window.go.main.App.GetVersion()
      .then((v: string) => setAppVersion('v' + v))
      .catch(() => {})
  }, [])

  // 启动时载入上次自动记忆的配置（仅记忆功能开启时；含环境路径），并尝试自动选中对应环境
  useEffect(() => {
    if (!memoryEnabled) return
    let alive = true
    window.go.main.App.LoadAutoConfig()
      .then((c) => {
        if (!alive) return
        // 防御：旧配置/损坏配置可能缺数组字段（null），先补成空数组避免渲染崩溃
        const fixed = {
          ...c,
          resources: Array.isArray(c.resources) ? c.resources : [],
          hiddenImports: Array.isArray(c.hiddenImports) ? c.hiddenImports : [],
          excludeModules: Array.isArray(c.excludeModules) ? c.excludeModules : [],
          extraArgs: Array.isArray(c.extraArgs) ? c.extraArgs : [],
        }
        setCfg((prev) => ({ ...prev, ...fixed }))
        if (c.pythonPath) {
          window.go.main.App.SearchEnvironments('')
            .then((envs) => {
              if (!alive) return
              const hit = envs.find((e) => e.path === c.pythonPath)
              if (hit) {
                setSelected(hit)
              } else {
                // 自动搜索找不到（例如自定义根目录下的环境）：按路径直接检测还原
                window.go.main.App.DetectPythonPath(c.pythonPath)
                  .then((e) => {
                    if (alive) setSelected(e)
                  })
                  .catch(() => {})
              }
            })
            .catch(() => {})
        }
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [])

  // 配置变化后自动保存到数据目录（仅记忆功能开启时，防抖 600ms，下次打开自动还原）
  useEffect(() => {
    if (!memoryEnabled) return
    const t = window.setTimeout(() => {
      window.go.main.App.SaveAutoConfig(cfg).catch(() => {})
    }, 600)
    return () => window.clearTimeout(t)
  }, [cfg, memoryEnabled])

  // 拖拽支持（页面级，仅在带 drop-zone 样式的页面区域生效）：
  // - 程序信息页（step 1）：.py 设为入口脚本；图片（png/jpg/jpeg/ico）设为程序图标；其他文件提示不支持
  // - 打包模式页（step 2）：拖入的任意文件/文件夹全部加入打包资源
  // useDropTarget=true：只有 drop 到 --wails-drop-target 元素上才触发，实现"按页面"而非全局
  const stepRef = useRef(step)
  useEffect(() => {
    stepRef.current = step
  }, [step])

  useEffect(() => {
    OnFileDrop((_x, _y, paths) => {
      const list = (paths || []).filter((p) => typeof p === 'string' && p.length > 0)
      if (list.length === 0) return
      if (stepRef.current === 1) {
        // 程序信息页：只认 .py 和图片
        const py = list.find((p) => p.toLowerCase().endsWith('.py'))
        const iconFile = list.find((p) => p !== py && /\.(png|jpe?g|ico)$/i.test(p))
        if (py) {
          setCfg((c) => ({ ...c, entryScript: py }))
          showToast(`已将「${baseName(py)}」设为入口脚本`)
        }
        if (iconFile) {
          setCfg((c) => ({ ...c, iconPath: iconFile }))
          showToast(`已将「${baseName(iconFile)}」设为程序图标`)
        }
        if (!py && !iconFile) {
          showToast('程序信息页仅支持拖入 .py 入口脚本或图片（png/jpg/ico）')
        }
        return
      }
      if (stepRef.current === 2) {
        // 打包模式页：全部作为资源
        Promise.all(list.map((p) => window.go.main.App.AddResource(p)))
          .then((items) => {
            setCfg((c) => {
              const exist = new Set(c.resources.map((r) => r.path))
              const add = items.filter((it) => exist.has(it.path) === false)
              return add.length > 0 ? { ...c, resources: [...c.resources, ...add] } : c
            })
            showToast(`已将 ${items.length} 个文件/文件夹添加为打包资源`)
          })
          .catch(() => showToast('资源添加失败，请检查文件是否存在'))
        return
      }
      // 其他页面：drop-zone 外不会触发，这里仅兜底
      showToast('请到「程序信息」或「打包模式」页面拖入文件')
    }, true)
    return () => {
      OnFileDropOff()
    }
  }, [])

  // 入口脚本变化时扫描其目录下的资源。
  // 注意：放在 App 层（而非 ModePage），组件切页不会重新扫描，避免覆盖用户对资源的勾选；
  // 扫描采用"合并"方式：已有路径保留用户的勾选状态，新增路径默认启用，手动添加的文件夹也保留。
  useEffect(() => {
    if (!cfg.entryScript) return
    let alive = true
    window.go.main.App.ScanResources(dirOf(cfg.entryScript)).then((items) => {
      if (!alive) return
      setCfg((c) => {
        const merged = [...c.resources]
        for (const it of items) {
          const idx = merged.findIndex((r) => r.path === it.path)
          if (idx >= 0) merged[idx] = { ...it, enabled: merged[idx].enabled }
          else merged.push({ ...it, enabled: true })
        }
        return { ...c, resources: merged }
      })
    })
    return () => {
      alive = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfg.entryScript])

  // 监听后端事件
  useEffect(() => {
    const offBuildLog = window.runtime.EventsOn('build:log', (d) =>
      setBuildLogs((l) => [...l, String(d)]),
    )
    const offDepsLog = window.runtime.EventsOn('deps:log', (d) =>
      setDepsLogs((l) => [...l, String(d)]),
    )
    const offDepsProgress = window.runtime.EventsOn('deps:progress', (d) => {
      const p = d as { percent?: number; stage?: string }
      if (typeof p === 'object' && p !== null) {
        setDepsProgress({ percent: Number(p.percent ?? 0), stage: String(p.stage ?? '') })
      }
    })
    // 依赖分析引擎的实时日志（PyInstaller 官方分析），独立通道避免混入安装日志
    const offDepsAnalysis = window.runtime.EventsOn('deps:analysis-log', (d) =>
      setAnalysisLogs((l) => [...l, String(d)]),
    )
    const offDone = window.runtime.EventsOn('build:done', (d) => {
      const payload = d as BuildDonePayload
      setDone(payload)
      setBuilding(false)
      // 若开启声音提示，按成功/失败播放对应提示音
      if (soundRef.current) playDoneSound(payload.success)
    })
    return () => {
      offBuildLog()
      offDepsLog()
      offDepsProgress()
      offDepsAnalysis()
      offDone()
    }
  }, [])

  // 选中环境变化时同步到配置
  useEffect(() => {
    if (selected) updateCfg({ pythonPath: selected.path })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected])

  const canComplete = (i: number): boolean => {
    switch (i) {
      case 0:
        return !!selected
      case 1:
        return !!(cfg.name.trim() && cfg.entryScript && cfg.outputDir)
      default:
        return true
    }
  }

  let maxReach = 0
  for (let i = 0; i < STEPS.length - 1; i++) {
    if (canComplete(i)) maxReach = i + 1
    else break
  }

  const startBuild = async () => {
    setBuildLogs([])
    setDone(null)
    setBuilding(true)
    try {
      await window.go.main.App.Build(cfg)
    } catch (e) {
      setBuildLogs((l) => [...l, String(e)])
      setBuilding(false)
    }
  }

  return (
    <ErrorBoundary>
      <div className="flex h-full flex-col bg-base">
      <header className="flex items-center justify-between px-8 pt-5">
        <div className="flex items-center gap-3">
          <img src={icon} alt="logo" className="h-10 w-10 rounded-xl shadow-card" />
          <div>
            <h1 className="text-sm font-semibold tracking-wide text-ink">Python打包工具</h1>
            <p className="text-[13px] text-ink-faint">🐨 一只树袋熊出品</p>
          </div>
        </div>
        <button
          type="button"
          className="rounded-full bg-surface-raised px-3 py-1 text-[13px] text-ink-soft transition-colors hover:border hover:border-line"
          title="关于本工具"
          onClick={() => setShowAbout(true)}
        >
          {appVersion}
        </button>
      </header>

      {showAbout && (
        <Modal onClose={() => setShowAbout(false)} maxW="max-w-md">
          <div className="flex items-center gap-4">
            <img src={icon} alt="logo" className="h-14 w-14 rounded-2xl shadow-card" />
            <div>
              <h2 className="text-lg font-semibold text-ink">Python打包工具</h2>
              <p className="text-sm text-ink-faint">🐨 一只树袋熊出品</p>
            </div>
          </div>
            <div className="mt-5 space-y-2 text-sm leading-relaxed text-ink-soft">
              <p>版本：{appVersion}</p>
              <p>
                一款面向 Python 新手的图形化打包工具：自动搜索 / 下载 Python 环境、
                自动分析并安装依赖、一键打包成 EXE。
              </p>
              <p>
                内置：单/多文件模式、UPX 体积压缩、PyArmor 代码加密、资源依赖分析、
                内置便携版 Python、中文报错诊断与修复建议。
              </p>
              <p>基于 Go + Wails + PyInstaller 构建。</p>
            </div>
            <div className="mt-5 rounded-panel border border-line bg-base p-3.5">
              <Toggle
                label="记忆功能（默认关闭）"
                hint={
                  memoryEnabled
                    ? '开启中：所有打包设置（含环境路径）自动保存，下次打开软件自动还原'
                    : '关闭：不保存也不还原设置'
                }
                checked={memoryEnabled}
                onChange={toggleMemory}
              />
              <div className="mt-3 border-t border-line pt-3">
                <p className="mb-2 text-xs text-ink-faint">保存/导入配置（不含 Python 路径）</p>
                <div className="flex gap-2">
                  <Button variant="ghost" size="sm" onClick={saveConfig}>
                    💾 保存配置
                  </Button>
                  <Button variant="ghost" size="sm" onClick={loadConfig}>
                    📂 导入配置
                  </Button>
                </div>
              </div>
            </div>
            <div className="mt-6 flex items-center justify-between gap-3">
              <Button variant="ghost" onClick={() => setShowAbout(false)}>
                关闭
              </Button>
              <div className="flex gap-2">
                <Button variant="ghost" onClick={() => BrowserOpenURL(APP_REPO_URL)}>
                  开源项目
                </Button>
                <Button variant="primary" onClick={() => BrowserOpenURL(APP_POST_URL)}>
                  更新帖子
                </Button>
              </div>
            </div>
        </Modal>
      )}

      {showExitConfirm && (
        <Modal onClose={() => setShowExitConfirm(false)} maxW="max-w-sm">
          <div className="flex items-center gap-3">
            <img src={icon} alt="" className="h-10 w-10 rounded-xl shadow-card" />
            <h2 className="text-base font-semibold text-ink">退出确认</h2>
          </div>
            <p className="mt-4 text-sm leading-relaxed text-ink-soft">
              程序正在执行中，关闭程序将终止当前的分析、安装、打包或下载进程，
              已完成的进度不会保留。确定要退出吗？
            </p>
            <div className="mt-6 flex justify-end gap-3">
              <Button variant="ghost" onClick={() => setShowExitConfirm(false)}>
                取消
              </Button>
              <Button variant="danger" onClick={confirmExitNow}>
                继续退出
              </Button>
            </div>
        </Modal>
      )}

      {/* 全局执行状态条：依赖分析 / 资源分析 / 安装依赖 / 打包构建 / 便携版下载 进行中，跨页面可见 */}
      {(analyzing || resAnalyzing || installing || building || portableDl) && (
        <div className="mx-8 mt-3 flex items-center gap-2.5 rounded-panel border border-accent/30 bg-accent/10 px-4 py-2.5 text-xs text-accent">
          <span className="inline-block h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" />
          <span className="font-medium">
            {analyzing
              ? '依赖分析中…'
              : resAnalyzing
                ? '资源分析中…'
                : installing
                  ? '安装依赖中…'
                  : building
                    ? '打包构建中…'
                    : '下载便携版 Python 中…'}
          </span>
          <span className="text-ink-faint">（分析/安装/构建/下载期间已锁定页面切换，请稍候）</span>
        </div>
      )}

      <StepNav current={step} onSelect={setStep} disabled={(i) => i > maxReach || busy} />

      <main className="flex-1 overflow-auto px-8 py-6">
        {step === 0 && (
          <EnvPage
            envs={envs}
            setEnvs={setEnvs}
            selected={selected}
            setSelected={setSelected}
            downloading={portableDl}
            setDownloading={setDownloading}
          />
        )}
        {step === 1 && <ConfigPage cfg={cfg} updateCfg={updateCfg} />}
        {step === 2 && (
          <ModePage
            cfg={cfg}
            updateCfg={updateCfg}
            resAnalyzing={resAnalyzing}
            setResAnalyzing={setResAnalyzing}
          />
        )}
        {step === 3 && (
          <DepsPage
            cfg={cfg}
            updateCfg={updateCfg}
            deps={deps}
            setDeps={setDeps}
            depsLogs={depsLogs}
            depsProgress={depsProgress}
            analysisLogs={analysisLogs}
            setAnalysisLogs={setAnalysisLogs}
            installing={installing}
            setInstalling={setInstalling}
            analyzing={analyzing}
            setAnalyzing={setAnalyzing}
            onCancelDeps={() => window.go.main.App.CancelDepsInstall()}
          />
        )}
        {step === 4 && (
          <BuildPage
            cfg={cfg}
            logs={buildLogs}
            done={done}
            building={building}
            soundEnabled={soundEnabled}
            onToggleSound={toggleSound}
            updateCfg={updateCfg}
            onBuild={startBuild}
            onCancel={() => window.go.main.App.CancelBuild()}
          />
        )}
      </main>

      <footer className="flex items-center justify-between border-t border-line px-8 py-4">
        <Button
          variant="ghost"
          onClick={() => setStep((s) => Math.max(0, s - 1))}
          disabled={step === 0 || busy}
        >
          ← 上一步
        </Button>
        {step < STEPS.length - 1 ? (
          <Button
            variant="primary"
            onClick={() => setStep((s) => Math.min(STEPS.length - 1, s + 1))}
            disabled={!canComplete(step) || busy}
          >
            下一步 →
          </Button>
        ) : (
          <span className="text-xs text-ink-faint">完成配置后点击「开始打包」</span>
        )}
      </footer>

      {/* 轻量提示（拖拽结果 / 配置导入导出结果） */}
      {toast && (
        <div className="pointer-events-none fixed bottom-20 left-1/2 z-[70] -translate-x-1/2 whitespace-nowrap rounded-full border border-line bg-surface-raised px-5 py-2.5 text-sm text-ink shadow-card">
          {toast}
        </div>
      )}
      </div>
    </ErrorBoundary>
  )
}

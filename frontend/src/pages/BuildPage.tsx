import { useEffect, useMemo, useRef, useState } from 'react'
import { Badge, Button, Card, Row, Toggle } from '../components/ui'
import LogPanel from '../components/LogPanel'
import type { BuildConfig, BuildDonePayload } from '../types/models'

// PyInstaller 打包阶段识别（按日志关键词匹配，用于展示进度，让用户知道打包仍在进行）
const STAGES = [
  { label: '初始化', min: 2, max: 6, match: /PyInstaller:|\[加密\]|开始打包/ },
  { label: '分析依赖与模块', min: 6, max: 30, match: /Analyzing/ },
  { label: '处理第三方库钩子', min: 30, max: 52, match: /Processing.*hook/ },
  { label: '收集动态库与资源', min: 52, max: 66, match: /Looking for|Adding data|add-data/ },
  { label: '构建 Python 归档', min: 66, max: 78, match: /Building PYZ|PYZ archive/ },
  { label: '生成可执行文件', min: 78, max: 97, match: /Building PKG|Building EXE|Building COLLECT|\.pkg/ },
  { label: '完成', min: 100, max: 100, match: /Build complete|completed successfully/ },
]

const LINES_PER_STAGE = 35 // 阶段内平滑推进到上限所需的日志行数

function fmtTime(total: number): string {
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

export default function BuildPage({
  cfg,
  logs,
  done,
  building,
  soundEnabled,
  onToggleSound,
  updateCfg,
  onBuild,
  onCancel,
}: {
  cfg: BuildConfig
  logs: string[]
  done: BuildDonePayload | null
  building: boolean
  soundEnabled: boolean
  onToggleSound: (v: boolean) => void
  updateCfg: (patch: Partial<BuildConfig>) => void
  onBuild: () => void
  onCancel: () => void
}) {
  const [stageIdx, setStageIdx] = useState(0)
  const [elapsed, setElapsed] = useState(0)
  const stageLineCount = useRef(0)
  const lastStage = useRef(-1)
  // 打包命令预览
  const [showCmd, setShowCmd] = useState(false)
  const [cmd, setCmd] = useState('')
  const [cmdLoading, setCmdLoading] = useState(false)
  const [cmdCopied, setCmdCopied] = useState(false)
  // 打包缓存加速折叠面板：始终默认关闭，点击按钮才展开
  const [showCacheAccel, setShowCacheAccel] = useState(false)
  // Python 环境展示名（带物理/虚拟标识），由后端检测生成
  const [envName, setEnvName] = useState('')

  // Python 路径变化时，向后端查询带物理/虚拟标识的展示名
  useEffect(() => {
    if (!cfg.pythonPath) {
      setEnvName('')
      return
    }
    let alive = true
    window.go.main.App.DetectPythonPath(cfg.pythonPath)
      .then((e) => {
        if (alive && e.name) setEnvName(e.name)
      })
      .catch(() => {
        /* 检测失败时退回显示原始路径 */
      })
    return () => {
      alive = false
    }
  }, [cfg.pythonPath])

  // 生成并展示将要执行的 PyInstaller 完整命令
  const previewCmd = async () => {
    if (cmd) {
      setShowCmd((v) => !v)
      return
    }
    setCmdLoading(true)
    try {
      const s = await window.go.main.App.PreviewCommand(cfg)
      setCmd(s)
      setShowCmd(true)
    } catch (e) {
      setCmd('生成命令失败：' + String(e))
      setShowCmd(true)
    } finally {
      setCmdLoading(false)
    }
  }

  // 复制打包命令到剪贴板
  const copyCmd = async () => {
    try {
      await window.runtime.ClipboardSetText(cmd)
      setCmdCopied(true)
      window.setTimeout(() => setCmdCopied(false), 2000)
    } catch {
      /* 忽略复制失败 */
    }
  }

  // 从实时日志解析当前打包阶段
  useEffect(() => {
    if (logs.length === 0) return
    const last = logs[logs.length - 1]
    let idx = -1
    STAGES.forEach((s, i) => {
      if (s.match.test(last)) idx = i
    })
    if (idx >= 0) {
      if (idx !== lastStage.current) {
        lastStage.current = idx
        stageLineCount.current = 0
      } else {
        stageLineCount.current += 1
      }
      setStageIdx(idx)
    } else {
      stageLineCount.current += 1
    }
  }, [logs])

  // 计时器：打包期间每秒 +1
  useEffect(() => {
    if (!building) {
      setElapsed(0)
      setStageIdx(0)
      lastStage.current = -1
      stageLineCount.current = 0
      return
    }
    const t = setInterval(() => setElapsed((e) => e + 1), 1000)
    return () => clearInterval(t)
  }, [building])

  // 阶段内按日志行数平滑推进，封顶到该阶段上限
  const percent = useMemo(() => {
    if (done?.success) return 100
    const s = STAGES[stageIdx]
    if (s.max >= 100) return 100
    const inner = Math.min(1, stageLineCount.current / LINES_PER_STAGE)
    return Math.min(s.max, Math.round(s.min + (s.max - s.min) * inner))
  }, [stageIdx, logs.length, done])

  return (
    <div className="space-y-5">
      <Card title="构建预览" desc="确认以下配置无误后开始打包；所有中间产物将在完成后自动清理">
        <div className="divide-y divide-line">
          <Row label="Python 环境" value={envName || cfg.pythonPath} />
          <Row label="程序名称" value={cfg.name} />
          <Row label="版本 / 作者" value={`${cfg.version || '—'} / ${cfg.author || '—'}`} />
          <Row label="入口脚本" value={cfg.entryScript} />
          <Row label="程序图标" value={cfg.iconPath || '未设置（使用默认图标）'} />
          <Row label="产物形态" value={cfg.oneFile ? '单文件' : '多文件'} />
          <Row label="控制台窗口" value={cfg.noConsole ? '隐藏' : '显示'} />
          <Row label="管理员权限" value={cfg.uacAdmin ? '需要（UAC 提权）' : '不需要'} />
          <Row label="打包模式" value={cfg.liteMode ? '精简' : '正常'} />
          <Row
            label="代码加密"
            value={
              cfg.encryptMode === 'none'
                ? '不加密'
                : cfg.encryptMode === 'deep'
                  ? '深度加密（JIT 动态指令保护）'
                  : '基础加密'
            }
          />
          <Row label="UPX 压缩" value={cfg.useUPX ? '开启' : '关闭'} />
          <Row
            label="隐式依赖导入"
            value={cfg.hiddenImports.length > 0 ? cfg.hiddenImports.join(', ') : '无'}
          />
          <Row
            label="排除模块"
            value={cfg.excludeModules.length > 0 ? cfg.excludeModules.join(', ') : '无'}
          />
          <Row
            label="自定义依赖文件夹"
            value={cfg.contentsDir ? cfg.contentsDir : '默认（_internal）'}
          />
          <Row label="附带资源" value={cfg.resources.filter((r) => r.enabled).length + ' 项'} />
          <Row label="输出目录" value={cfg.outputDir} />
        </div>
        {/* 开始打包：居中放大；打包中按钮呼吸动画（进度条另有流动动画） */}
        <div className="mt-6 flex items-center justify-center gap-4">
          <Button
            variant="primary"
            onClick={onBuild}
            disabled={building}
            className={building ? '!h-12 !px-12 !text-base animate-pulse' : '!h-12 !px-12 !text-base'}
          >
            {building ? '⏳ 正在打包…' : '🚀 开始打包'}
          </Button>
          {building && (
            <Button variant="danger" onClick={onCancel} className="!h-12 !px-6 !text-base">
              ⏹ 取消
            </Button>
          )}
        </div>

        {/* 高级选项折叠按钮：与打包模式页「代码加密 / UPX 压缩」样式统一（ghost + emoji + 展开收起） */}
        <div className="mt-4 flex flex-wrap gap-2">
          <Button variant="ghost" onClick={previewCmd} disabled={cmdLoading}>
            {cmdLoading ? '⏳ 生成中…' : showCmd ? '▾ 收起打包命令' : '📋 查看打包命令'}
          </Button>
          <Button variant="ghost" onClick={() => setShowCacheAccel((v) => !v)}>
            {showCacheAccel ? '▾ 收起打包缓存加速' : '⚡ 打包缓存加速'}
          </Button>
        </div>

        {showCmd && cmd && (
          <div className="mt-3 rounded-panel border border-line bg-base p-3">
            <pre className="max-h-44 select-text overflow-auto whitespace-pre-wrap break-all text-xs leading-relaxed text-ink-soft">
              {cmd}
            </pre>
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <Button variant="ghost" size="sm" onClick={copyCmd}>
                {cmdCopied ? '✓ 已复制' : '📋 复制命令'}
              </Button>
              <span className="text-[11px] text-ink-faint">
                启用代码加密时实际命令与预览略有差异（先加密再打包）
              </span>
            </div>
          </div>
        )}

        {showCacheAccel && (
          <div className="mt-3 rounded-panel border border-line bg-base p-4">
            <Toggle
              label="打包缓存加速"
              hint="开启（默认）：使用编译缓存加速打包；关闭：全新构建并清理缓存"
              checked={cfg.cacheAccel}
              onChange={(v) => updateCfg({ cacheAccel: v })}
            />
            <p className="mt-2.5 text-xs leading-relaxed text-ink-soft">
              <span className="font-medium text-ink">开关影响：</span>
              <br />
              <span className="font-medium text-good">开启（默认）：</span>
              复用 PyInstaller 编译缓存，同一台电脑重复打包（尤其是 PySide6 这类大项目）时，
              每次可省 1~3 分钟；缓存保留在系统缓存目录（约几十~几百 MB）。
              <br />
              <span className="font-medium text-warn">关闭：</span>
              本次打包前强制全新构建（等效 PyInstaller --clean），打包完成后立即删除缓存，
              下次打包也不受旧缓存影响。适合升级依赖、更换 Python 版本后打包出现异常时关闭一次。
              <br />
              清理范围仅限 PyInstaller 缓存，<span className="font-medium">不会动你的 Python 环境和依赖包</span>。
            </p>
          </div>
        )}
        <div className="mt-4 border-t border-line pt-4">
          <Toggle
            label="打包完成声音提示"
            hint="打包成功/失败时播放提示音，开关实时生效"
            checked={soundEnabled}
            onChange={onToggleSound}
          />
        </div>
      </Card>

      <Card title="打包进度" desc="实时展示打包进度与已用时，日志可在下方查看与复制">
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-ink">
              {building ? STAGES[stageIdx].label : '尚未开始'}
            </span>
            <span className="text-sm text-ink-faint">
              {building ? `${percent}% · 已用时 ${fmtTime(elapsed)}` : '点击「开始打包」后自动更新'}
            </span>
          </div>
          <div className="h-4 w-full overflow-hidden rounded-full bg-line">
            <div
              className="progress-animated h-full rounded-full bg-good transition-all duration-500"
              style={{ width: `${building ? percent : 0}%` }}
            />
          </div>
          {building && (
            <p className="text-xs text-ink-faint">
              正在「{STAGES[stageIdx].label}」。含 PyTorch / TensorRT / PySide6 等项目可能耗时
              10~30 分钟，进度条与日志滚动表示仍在正常打包。
            </p>
          )}
        </div>
      </Card>

      {done && (
        <Card title={done.success ? '打包完成' : '打包失败'} desc={done.message}>
          {done.success && done.artifacts.length > 0 && (
            <ul className="space-y-1.5">
              {done.artifacts.map((a) => (
                <li
                  key={a}
                  className="flex items-center gap-2 rounded-field border border-line bg-base px-3 py-2 text-sm"
                >
                  <Badge tone="good">产物</Badge>
                  <span className="truncate text-ink">{a}</span>
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}

      {done?.success && (
        <Card title="发给别人前，请留意这几点">
          <ul className="space-y-2.5 text-xs leading-relaxed text-ink-soft">
            <li>
              <span className="font-medium text-ink">🔧 对方电脑报错缺 DLL？</span>
              如果运行时提示缺少 VCRUNTIME140.dll 等文件，是对方电脑缺少微软 VC++ 运行库，
              安装「Microsoft Visual C++ Redistributable」即可解决（软件管家/官网可下载）。
            </li>
            <li>
              <span className="font-medium text-ink">🛡️ 杀毒软件拦截？</span>
              360、火绒、Windows Defender 等可能对 PyInstaller 打包的程序误报，
              让对方把程序加入信任/白名单即可；如果开了 UPX 压缩，关闭后重新打包可降低误报率。
            </li>
            <li>
              <span className="font-medium text-ink">🐢 单文件启动慢？</span>
              单文件模式启动时会先把程序解压到临时目录，慢几秒属正常现象，不是卡死。
            </li>
            <li>
              <span className="font-medium text-ink">✅ 分发前先自测</span>
              建议先在自己电脑上双击运行一遍，确认功能正常再发给别人。
            </li>
            <li>
              <span className="font-medium text-ink">🖼️ 资源管理器里图标还是旧的？</span>
              打包的图标其实已嵌入文件，是 Windows 资源管理器缓存了旧图标。把 exe
              复制/改名到别处即可看到新图标，或重启资源管理器（任务管理器结束
              explorer.exe 再重新打开）后刷新。
            </li>
          </ul>
        </Card>
      )}

      <Card title="构建日志">
        <LogPanel logs={logs} height="h-72" />
      </Card>
    </div>
  )
}

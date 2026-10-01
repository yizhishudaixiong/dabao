import { useState } from 'react'
import { Button, Card, Seg, Toggle } from '../components/ui'
import type { BuildConfig, ResourceDep, ResourceItem } from '../types/models'
import { baseName, dirOf } from '../lib/path'

// 加密档位说明文案（与后端 EncryptMode 取值对齐：none / basic / deep）
const ENC_INFO: Record<string, { title: string; desc: string }> = {
  none: {
    title: '不加密',
    desc: '直接打包，运行速度快、体积最小。代码以字节码明文存放，可用反编译工具提取出源码。适合内部工具或对源码保护无要求的场景。',
  },
  basic: {
    title: '基础加密（推荐）',
    desc: '基于 PyArmor 对 Python 字节码做高强度加密 + 代码对象保护，可有效阻止绝大多数反编译工具扒出源码，体积仅略有增加。',
  },
  deep: {
    title: '深度加密',
    desc: '在基础加密之上叠加 JIT 动态指令保护、函数调用校验、属性访问保护，防破解能力更强，适合商业分发（体积略增、启动略慢）。',
  },
}

// 计算资源打包后的目标路径（与后端 resourceTarget 规则保持一致）：
// - 程序目录内：保留相对入口脚本目录的层级
// - 程序目录外：文件放资源根；文件夹保留文件夹本身及内部结构
function packedPath(r: ResourceItem, entryScript: string): string {
  const base = baseName(r.path)
  if (!entryScript) return base
  const projDir = dirOf(entryScript).toLowerCase()
  const d = dirOf(r.path).toLowerCase()
  if (d === projDir) return base
  if (d.startsWith(projDir + '\\')) return r.path.slice(projDir.length + 1)
  return base
}

export default function ModePage({
  cfg,
  updateCfg,
  resAnalyzing,
  setResAnalyzing,
}: {
  cfg: BuildConfig
  updateCfg: (patch: Partial<BuildConfig>) => void
  resAnalyzing: boolean
  setResAnalyzing: (v: boolean) => void
}) {
  // 注意：入口脚本目录的资源扫描由 App 层统一负责（仅入口变化时执行），
  // 这里只负责展示与勾选操作，避免切页时把用户✕ 取消勾选的资源重新勾上。
  // 自定义依赖文件夹名称默认隐藏，点击按钮展开（已有内容时默认展开）
  const [showContentsDir, setShowContentsDir] = useState(cfg.contentsDir.length > 0)
  // 代码加密 / UPX 压缩默认隐藏，点击按钮展开（已启用时默认展开）
  const [showEncrypt, setShowEncrypt] = useState(cfg.encryptMode !== 'none')
  const [showUPX, setShowUPX] = useState(cfg.useUPX)

  const toggleResource = (path: string) => {
    updateCfg({
      resources: cfg.resources.map((r) => (r.path === path ? { ...r, enabled: !r.enabled } : r)),
    })
  }

  // 手动选择一个文件夹作为额外打包资源
  const addFolder = async () => {
    try {
      const dir = await window.go.main.App.SelectResourceDir()
      if (!dir) return
      const item = await window.go.main.App.AddResource(dir)
      if (cfg.resources.some((r) => r.path === item.path)) return
      updateCfg({ resources: [...cfg.resources, item] })
    } catch (e) {
      console.error('选择文件夹失败', e)
    }
  }

  // 手动选择一个文件作为额外打包资源
  const addFile = async () => {
    try {
      const file = await window.go.main.App.SelectResourceFile()
      if (!file) return
      const item = await window.go.main.App.AddResource(file)
      if (cfg.resources.some((r) => r.path === item.path)) return
      updateCfg({ resources: [...cfg.resources, item] })
    } catch (e) {
      console.error('选择文件失败', e)
    }
  }

  // 全选 / 全不选（资源较多时一键切换）
  const allChecked = cfg.resources.length > 0 && cfg.resources.every((r) => r.enabled)
  const toggleAll = () => {
    updateCfg({ resources: cfg.resources.map((r) => ({ ...r, enabled: !allChecked })) })
  }

  // 移除资源：只从打包清单中删除路径，不删除磁盘上的文件
  const removeResource = (path: string) => {
    updateCfg({ resources: cfg.resources.filter((r) => r.path !== path) })
  }

  // 资源依赖分析：对比代码引用的资源与已勾选清单
  const [resDeps, setResDeps] = useState<ResourceDep[]>([])
  const [showResDeps, setShowResDeps] = useState(false)
  const [resDepsMsg, setResDepsMsg] = useState('')

  const analyzeResDeps = async () => {
    if (!cfg.entryScript) {
      showTip('请先在「程序信息」中选择入口脚本')
      return
    }
    if (!cfg.pythonPath) {
      showTip('请先在「选择环境」中选择 Python 环境（资源分析需要 Python）')
      return
    }
    setResAnalyzing(true)
    setResDepsMsg('')
    try {
      const list = await window.go.main.App.AnalyzeResourceDeps(
        cfg.pythonPath,
        cfg.entryScript,
        cfg.resources,
      )
      setResDeps(list)
      setShowResDeps(true)
      if (list.length === 0) setResDepsMsg('未发现代码引用的资源路径（或已全部包含）')
    } catch (e) {
      setResDepsMsg('分析失败：' + String(e))
      setShowResDeps(true)
    } finally {
      setResAnalyzing(false)
    }
  }

  // 按当前入口脚本重新添加资源：清空旧列表（含上个项目遗留），按入口脚本目录重新扫描
  const [confirmReset, setConfirmReset] = useState(false)
  const [tip, setTip] = useState('')
  const showTip = (msg: string) => {
    setTip(msg)
    window.setTimeout(() => setTip(''), 3000)
  }

  const resetResources = async () => {
    if (!cfg.entryScript) {
      showTip('请先在「程序信息」中选择入口脚本')
      return
    }
    setConfirmReset(true)
  }

  const doResetResources = async () => {
    setConfirmReset(false)
    try {
      const items = await window.go.main.App.ScanResources(dirOf(cfg.entryScript))
      updateCfg({ resources: items })
      showTip(`已按入口脚本目录重置资源（${items.length} 项）`)
    } catch (e) {
      showTip('资源重置失败：' + String(e))
    }
  }

  return (
    <div className="drop-zone space-y-5">
      <Card
        title="打包模式"
        desc="选择产物的形态：单文件便于分发（一个 exe 发给任何人就能用）；多文件启动更快、体积更小（一个文件夹）"
      >
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-ink">产物形态</p>
            <p className="mt-0.5 text-xs text-ink-faint">
              新手建议用「单文件」：复制给别人一个文件即可，不用带整个文件夹
            </p>
          </div>
          <Seg
            value={cfg.oneFile ? 'one' : 'multi'}
            onChange={(v) => updateCfg({ oneFile: v === 'one' })}
            options={[
              { value: 'one', label: '单文件' },
              { value: 'multi', label: '多文件' },
            ]}
          />
        </div>
        {!cfg.oneFile && (
          <div className="mt-4">
            <Button variant="ghost" onClick={() => setShowContentsDir((v) => !v)}>
              {showContentsDir ? '▾ 收起自定义依赖文件夹名称' : '＋ 自定义依赖文件夹名称'}
            </Button>
            {showContentsDir && (
              <div className="mt-3 rounded-panel border border-line bg-base p-4">
                <label className="block text-xs font-medium text-ink-soft">
                  自定义依赖文件夹名称（多文件模式）
                </label>
                <input
                  className="field mt-1.5"
                  placeholder="_internal（留空使用默认名）"
                  value={cfg.contentsDir}
                  onChange={(e) => updateCfg({ contentsDir: e.target.value })}
                />
                <p className="mt-1.5 text-xs text-ink-faint">
                  程序依赖存放的文件夹名。例如填 deps 后，产物结构为 应用名.exe + deps/ 文件夹。
                  建议使用英文名称（个别依赖可能出现中文兼容错误）。
                </p>
              </div>
            )}
          </div>
        )}
      </Card>

      <Card
        title="运行方式"
        desc="控制程序打包后怎么启动、体积怎么取舍"
      >
        <div className="grid grid-cols-2 gap-3">
          <Toggle
            label="显示控制台窗口"
            hint="关闭（默认）：程序运行时隐藏黑色控制台窗口，适合有界面的程序；开启：显示命令行窗口，适合需要打印日志的程序"
            checked={!cfg.noConsole}
            onChange={(v) => updateCfg({ noConsole: !v })}
          />
          <Toggle
            label="精简打包"
            hint="排除 tkinter 等用不到的标准库模块，进一步减小体积"
            checked={cfg.liteMode}
            onChange={(v) => updateCfg({ liteMode: v })}
          />
          <Toggle
            label="管理员权限运行"
            hint="开启：程序启动时自动请求管理员权限；关闭（默认）：以普通用户权限运行"
            checked={cfg.uacAdmin}
            onChange={(v) => updateCfg({ uacAdmin: v })}
          />
        </div>
        {cfg.uacAdmin && (
          <p className="mt-3 rounded-panel border border-warn/30 bg-warn/15 px-4 py-2.5 text-xs text-ink-soft">
            <span className="font-medium text-warn">风险提示：</span>
            请求管理员权限的程序更容易被杀毒软件警惕；用户电脑上启动时会弹出
            「用户账户控制（UAC）」确认框。仅当程序确实需要写系统目录、修改注册表、
            管理其他软件时才建议开启。
          </p>
        )}
        {cfg.liteMode && (
          <p className="mt-3 rounded-panel border border-warn/30 bg-warn/15 px-4 py-2.5 text-xs text-ink-soft">
            <span className="font-medium text-warn">风险提示：</span>
            精简打包可能会少打一些隐式依赖。如果打包后的程序运行时提示缺少模块，请关闭「精简打包」后重新打包。
          </p>
        )}
      </Card>

      {/* 代码加密 / UPX 压缩：默认折叠，点击按钮展开（一行两个按钮） */}
      <div className="flex flex-wrap gap-2">
        <Button variant="ghost" onClick={() => setShowEncrypt((v) => !v)}>
          {showEncrypt ? '▾ 收起代码加密' : '🔐 代码加密'}
        </Button>
        <Button variant="ghost" onClick={() => setShowUPX((v) => !v)}>
          {showUPX ? '▾ 收起 UPX 压缩' : '🗜 UPX 压缩'}
        </Button>
      </div>

      {showEncrypt && (
      <Card
        title="代码加密"
        desc="选择是否保护打包产物的源代码（基于 PyArmor 免费版即可；加密范围=项目中所有自写代码模块，第三方依赖不加密）"
      >
        <Seg
          value={cfg.encryptMode}
          onChange={(v) => updateCfg({ encryptMode: v })}
          options={[
            { value: 'none', label: '不加密' },
            { value: 'basic', label: '基础加密' },
            { value: 'deep', label: '深度加密' },
          ]}
        />
        <div className="mt-4 rounded-panel border border-line bg-base p-4">
          <p className="text-sm font-medium text-ink">{ENC_INFO[cfg.encryptMode]?.title}</p>
          <p className="mt-1 text-xs leading-relaxed text-ink-soft">
            {ENC_INFO[cfg.encryptMode]?.desc}
          </p>
        </div>

        <p className="mt-3 text-xs leading-relaxed text-ink-faint">
          提醒：加密能有效阻止「反编译提取源码」，但无法防御「动态调试 + 内存转储」等极高级破解手段——对绝大多数商业场景已经足够。
          <br />
          <span className="font-medium text-warn">试用版限制：</span>
          PyArmor 免费版无法加密单个代码对象超过 32KB 的超大文件（如某个 .py 特别大或包含超大函数）。此类文件会自动跳过加密并以明文打包，构建日志会列出未加密的文件清单；入口脚本超限则会直接报错。
        </p>
      </Card>
      )}

      {showUPX && (
      <Card
        title="UPX 压缩"
        desc="压缩产物以减小体积（可选，默认关闭）"
      >
        <Toggle
          label="启用 UPX 压缩"
          hint="使用内置的 UPX 压缩器压缩产物，体积通常能减小 30%~50%，无需联网下载"
          checked={cfg.useUPX}
          onChange={(v) => updateCfg({ useUPX: v })}
        />
        <p className="mt-3 rounded-panel border border-line bg-base px-4 py-2.5 text-xs leading-relaxed text-ink-soft">
          <span className="font-medium text-ink">说明：</span>
          勾选后，打包好的程序运行时先解压再启动，所以体积变小、启动会稍慢一点点（通常可忽略）。
          适合需要把程序发给别人、对体积敏感的场景。
        </p>
        {cfg.useUPX && (
          <p className="mt-3 rounded-panel border border-warn/30 bg-warn/15 px-4 py-2.5 text-xs text-ink-soft">
            <span className="font-medium text-warn">风险提示：</span>
            UPX 压缩后的文件带有压缩壳特征，<span className="font-medium text-warn">可能更容易被杀毒软件误报</span>。
            如果打包好的程序在别人电脑上被杀毒拦截，请关闭「UPX 压缩」重新打包。
          </p>
        )}
      </Card>
      )}

      <Card
        title="资源一起打包"
        desc="勾选需要随程序一起打包的资源（图片、配置文件、素材、文件夹等），下方显示打包后的位置，代码请按该位置读取"
      >
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap gap-2">
            <Button variant="ghost" onClick={addFolder}>
              📁 选择额外资源文件夹
            </Button>
            <Button variant="ghost" onClick={addFile}>
              📄 选择额外资源文件
            </Button>
            <Button variant="ghost" onClick={analyzeResDeps} disabled={resAnalyzing}>
              {resAnalyzing ? '分析中…' : '🔍 分析程序资源依赖'}
            </Button>
            <Button variant="ghost" onClick={resetResources}>
              🔄 按入口脚本重新添加资源
            </Button>
          </div>
          {cfg.resources.length > 0 && (
            <Button variant="ghost" onClick={toggleAll}>
              {allChecked ? '☐ 全不选' : '☑ 全选'}
            </Button>
          )}
        </div>
        {tip && <p className="mb-3 rounded-panel border border-line bg-base px-3 py-2 text-xs text-ink-soft">{tip}</p>}
        {showResDeps && (
          <div className="mb-3 rounded-panel border border-line bg-base p-3.5">
            <div className="flex items-center justify-between">
              <p className="text-xs font-medium text-ink">资源依赖分析结果</p>
              <Button variant="ghost" size="sm" onClick={() => setShowResDeps(false)}>
                ▾ 收起
              </Button>
            </div>
            {resDepsMsg && <p className="mt-1.5 text-xs text-ink-faint">{resDepsMsg}</p>}
            {resDeps.length > 0 && (
              <ul className="mt-2.5 max-h-64 space-y-1.5 overflow-auto pr-1">
                {resDeps.map((d, i) => (
                  <li
                    key={i}
                    className="rounded-field border border-line bg-base px-3 py-2"
                  >
                    <div className="flex items-center gap-2.5">
                      {d.included ? (
                        <span className="shrink-0 rounded-md bg-good/15 px-2 py-0.5 text-[11px] font-medium text-good">
                          ✓ 已包含
                        </span>
                      ) : (
                        <span className="shrink-0 rounded-md bg-bad/15 px-2 py-0.5 text-[11px] font-medium text-bad">
                          ✗ 未包含
                        </span>
                      )}
                      <span className="min-w-0 flex-1 truncate text-sm text-ink" title={d.ref}>
                        {d.ref}
                      </span>
                      {d.sourceFile && (
                        <span className="shrink-0 truncate rounded bg-base px-1.5 py-0.5 text-[11px] text-ink-faint">
                          来自 {d.sourceFile}
                          {d.line > 0 ? ` 第 ${d.line} 行` : ''}
                        </span>
                      )}
                    </div>
                    {d.kind === 'file' && (
                      <p
                        className={`mt-1 break-all text-[11px] leading-relaxed ${
                          d.exists ? 'text-ink-faint' : 'text-warn'
                        }`}
                      >
                        {d.exists ? d.diskPath : '（磁盘上未找到）'}
                      </p>
                    )}
                    {d.code && (
                      <p className="mt-1 truncate rounded bg-base px-1.5 py-1 font-mono text-[11px] text-ink-soft ring-1 ring-line" title={d.code}>
                        {d.code}
                      </p>
                    )}
                  </li>
                ))}
              </ul>
            )}
            <div className="mt-3 space-y-1.5 border-t border-line pt-3 text-xs leading-relaxed text-ink-faint">
              <p>
                <span className="font-medium text-ink">什么是资源依赖：</span>
                程序运行时需要读取的文件（图片 / 配置 / 数据等）。打包时没包含它，运行到读取处就会报
                「找不到文件」。
              </p>
              <p>
                <span className="font-medium text-ink">代码引用资源的方式：</span>
                ① 直接相对路径
                <code className="mx-1 rounded bg-base px-1 py-0.5 text-[11px]">open('logo.png')</code>
                开发时能跑，打包后工作目录变了容易失效，建议用
                <code className="mx-1 rounded bg-base px-1 py-0.5 text-[11px]">__file__</code>
                拼接；②
                <code className="mx-1 rounded bg-base px-1 py-0.5 text-[11px]">os.path.dirname(__file__)</code>
                拼接：程序目录内的资源保留层级，单文件打包后需改用
                <code className="mx-1 rounded bg-base px-1 py-0.5 text-[11px]">sys._MEIPASS</code>
                。
              </p>
              <p className="rounded-panel border border-warn/30 bg-warn/15 px-3 py-2 text-warn">
                <span className="font-medium">请注意：</span>
                本功能是静态启发式分析，<span className="font-medium">可能误报（把非资源当成资源），也可能漏报</span>
                （变量拼接、运行时生成的路径识别不了）。结果仅供提醒，
                请自行判断，最终以打包后运行测试为准。
              </p>
              <p>
                <span className="font-medium text-ink">扫描范围：</span>
                用当前 Python 环境解析入口脚本目录下所有
                <code className="mx-1 rounded bg-base px-1 py-0.5 text-[11px]">.py</code>
                文件的读取调用（open / cv2.imread / np.load / torch.load / os.path.join / Path 等），
                自动跳过虚拟环境、缓存、构建产物。每条标注来源文件与行号，方便定位。
              </p>
              <p>
                <span className="font-medium text-ink">怎么看结果：</span>
                绿色「已包含」= 代码引用的资源已被勾选，放心；红色「未包含」= 引用了但没勾选，
                运行时可能报「找不到文件」，建议补勾选。动态路径（用变量拼路径）
                和 Windows 系统库（如 user32.dll）不在此列，请忽略。
              </p>
            </div>
          </div>
        )}
        {cfg.resources.length === 0 ? (
          <p className="rounded-panel border border-dashed border-line p-6 text-center text-xs text-ink-faint">
            暂无资源。可在「程序信息」中选择入口脚本后自动扫描其目录，
            点击上方按钮手动添加额外资源，或直接把文件/文件夹拖进本页。
          </p>
        ) : (
          <ul className="max-h-56 space-y-1.5 overflow-auto pr-1">
            {cfg.resources.map((r: ResourceItem) => {
              const target = packedPath(r, cfg.entryScript)
              return (
                <li key={r.path} className="flex items-center gap-1.5">
                  <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 rounded-field border border-line bg-base px-3 py-2 transition-colors hover:border-ink-faint">
                    <input
                      type="checkbox"
                      checked={r.enabled}
                      onChange={() => toggleResource(r.path)}
                      className="h-4 w-4 accent-accent"
                    />
                    <span className="shrink-0 text-base leading-none">{r.isDir ? '📁' : '📄'}</span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm text-ink">{r.path}</span>
                      <span className="block truncate text-[11px] text-ink-faint">
                        打包后：{r.isDir ? target + '\\' : target}
                      </span>
                    </span>
                  </label>
                  <button
                    onClick={() => removeResource(r.path)}
                    title="从打包清单移除（不会删除磁盘上的文件）"
                    className="flex h-7 w-7 shrink-0 items-center justify-center rounded-field text-sm text-ink-faint transition-colors hover:bg-bad/10 hover:text-bad"
                  >
                    ✕
                  </button>
                </li>
              )
            })}
          </ul>
        )}
        <p className="mt-3 text-xs leading-relaxed text-ink-faint">
          <span className="font-medium text-ink">代码怎么读资源？</span>
          程序目录内的资源保留原层级（如 assets\logo.png），代码用 os.path.join(
          os.path.dirname(__file__), 'assets', 'logo.png') 即可；
          外部添加的文件/文件夹在资源根，代码用 sys._MEIPASS 拼接（如
          os.path.join(sys._MEIPASS, 'photo.png')）。
        </p>
      </Card>

      {/* 按入口脚本重置资源：确认弹窗 */}
      {confirmReset && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-6">
          <div className="card w-full max-w-sm p-7 shadow-card">
            <div className="flex items-center gap-3">
              <span className="text-xl">🔄</span>
              <h2 className="text-base font-semibold text-ink">重置资源清单</h2>
            </div>
            <p className="mt-4 text-sm leading-relaxed text-ink-soft">
              将清空当前资源列表（包括上个项目遗留、手动添加的额外资源），
              并按当前入口脚本所在目录重新扫描添加。确定继续吗？
            </p>
            <div className="mt-6 flex justify-end gap-3">
              <Button variant="ghost" onClick={() => setConfirmReset(false)}>
                ✕ 取消
              </Button>
              <Button variant="primary" onClick={doResetResources}>
                🔄 确定重置
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

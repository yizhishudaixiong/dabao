import { useEffect, useRef, useState, type Dispatch, type SetStateAction } from 'react'
import { Badge, Button, Card, Select } from '../components/ui'
import type { PortablePython, PortableVersionInfo, PythonEnv } from '../types/models'
import { PY_DOWNLOAD_URL, PY_DIRECT_URL } from '../constants'

const CUSTOM = '__custom__'

export default function EnvPage({
  envs,
  setEnvs,
  selected,
  setSelected,
  downloading,
  setDownloading,
}: {
  envs: PythonEnv[]
  setEnvs: Dispatch<SetStateAction<PythonEnv[]>>
  selected: PythonEnv | null
  setSelected: (e: PythonEnv | null) => void
  // 下载便携版是否进行中（由 App 层统一忙碌状态传入：下载期间禁用切页 + 后端拦截退出）
  downloading: boolean
  setDownloading: (v: boolean) => void
}) {
  const [root, setRoot] = useState('')
  const [searching, setSearching] = useState(false)
  const [msg, setMsg] = useState('')
  const [searched, setSearched] = useState(false)

  // 新建虚拟环境表单状态
  const [basePython, setBasePython] = useState('')
  const [venvDir, setVenvDir] = useState('')
  const [venvName, setVenvName] = useState('venv_py')
  const [creating, setCreating] = useState(false)
  const [createMsg, setCreateMsg] = useState('')

  // 复制按钮反馈：'' 未复制 | 'url' 复制了下载页 | 'direct' 复制了直链
  const [copied, setCopied] = useState<'url' | 'direct' | ''>('')
  // 新建虚拟环境表单是否展开（默认收起，点击按钮才展示）
  const [showCreate, setShowCreate] = useState(false)

  // ---- 便携版下载面板状态（downloading 由 App 层统一忙碌状态传入）----
  const [showDl, setShowDl] = useState(false)
  const [dlVersions, setDlVersions] = useState<PortableVersionInfo[]>([])
  const [dlVersion, setDlVersion] = useState('')
  const [dlProgress, setDlProgress] = useState<{
    percent: number
    doneMB: number
    totalMB: number
    speedMBps: number
    stage: string
  } | null>(null)
  const [dlLogs, setDlLogs] = useState<string[]>([])
  const [dlMsg, setDlMsg] = useState('')
  const speedRef = useRef({ t: 0, mb: 0 })

  // ---- 清理便携版面板状态 ----
  const [showClean, setShowClean] = useState(false)
  const [cleanList, setCleanList] = useState<PortablePython[]>([])
  const [cleanMsg, setCleanMsg] = useState('')
  const [deleting, setDeleting] = useState('')
  // 便携版存放根目录（用于下载面板说明展示）
  const [portableRoot, setPortableRoot] = useState('')

  // 复制到剪贴板
  const copy = async (key: 'url' | 'direct') => {
    try {
      await navigator.clipboard.writeText(key === 'url' ? PY_DOWNLOAD_URL : PY_DIRECT_URL)
      setCopied(key)
      setTimeout(() => setCopied(''), 2000)
    } catch {
      setMsg('复制失败，请手动选中地址复制')
    }
  }

  // 浏览按钮：弹出文件夹选择框，选择自定义环境根目录
  const browseRoot = async () => {
    const dir = await window.go.main.App.SelectCustomRoot()
    if (dir) setRoot(dir)
  }

  // 自动搜索：扫描系统环境变量 + 自定义环境根目录下的虚拟环境 + 已下载的便携版
  const search = async () => {
    setSearching(true)
    setMsg('')
    try {
      const list = await window.go.main.App.SearchEnvironments(root.trim())
      setEnvs(list)
      setSearched(true)
      // 自动选中：仅当前未选中（或选中项已不在列表）时生效。
      // ① 列表里恰好只有一个环境（无论物理/虚拟/便携版）→ 直接选中，省去手动选择；
      //    否则只含一个虚拟环境时 selected 会保持 null，下拉框单选项将无法被点击选中。
      // ② 有多个时，若其中只有一个非虚拟环境（物理或内置便携版），优先选中它。
      if (!selected || !list.some((e) => e.path === selected.path)) {
        if (list.length === 1) {
          setSelected(list[0])
        } else {
          const standalone = list.filter((e) => !e.isVirtual)
          if (standalone.length === 1) {
            setSelected(standalone[0])
          } else if (selected) {
            setSelected(null)
          }
        }
      }
      setMsg(
        list.length === 0
          ? '未搜索到 Python 环境，可以点击下方「下载便携版 Python」自动获取，或手动选择 python.exe'
          : `共发现 ${list.length} 个环境`,
      )
    } catch (e) {
      setMsg('搜索失败: ' + String(e))
    } finally {
      setSearching(false)
    }
  }

  // 下载面板：点击展开/收起（下载中禁止收起），展开时加载版本清单与存放目录
  const openDl = async () => {
    if (downloading) return
    if (showDl) {
      setShowDl(false)
      return
    }
    try {
      const [vs, rootDir] = await Promise.all([
        window.go.main.App.GetPortableVersions(),
        window.go.main.App.GetPortableRootDir(),
      ])
      setDlVersions(vs)
      setDlVersion(vs.find((v) => v.default)?.version ?? vs[0]?.version ?? '')
      setPortableRoot(rootDir)
    } catch {
      /* 忽略：版本清单加载失败时面板内仍可手动输入 */
    }
    setDlLogs([])
    setDlProgress(null)
    setDlMsg('')
    setShowDl(true)
  }

  // 开始下载
  const startDl = async () => {
    if (!dlVersion) return
    setDownloading(true)
    setDlLogs([])
    setDlProgress({ percent: 0, doneMB: 0, totalMB: 0, speedMBps: 0, stage: '准备中' })
    setDlMsg('')
    speedRef.current = { t: Date.now(), mb: 0 }
    try {
      await window.go.main.App.DownloadPortablePython(dlVersion)
    } catch (e) {
      setDlMsg('启动下载失败: ' + String(e))
      setDownloading(false)
    }
  }

  const cancelDl = () => {
    window.go.main.App.CancelPortableDownload()
  }

  // 监听便携版下载事件（日志 / 进度 / 完成）
  useEffect(() => {
    const offLog = window.runtime.EventsOn('portable:log', (d) =>
      setDlLogs((l) => [...l, String(d)]),
    )
    const offProg = window.runtime.EventsOn('portable:progress', (d) => {
      const p = d as { percent?: number; doneMB?: number; totalMB?: number; stage?: string }
      const now = Date.now()
      const mb = Number(p.doneMB ?? 0)
      const dt = (now - speedRef.current.t) / 1000
      let speed = 0
      if (dt >= 0.8) {
        speed = (mb - speedRef.current.mb) / dt
        speedRef.current = { t: now, mb }
      }
      setDlProgress({
        percent: Number(p.percent ?? 0),
        doneMB: mb,
        totalMB: Number(p.totalMB ?? 0),
        speedMBps: speed,
        stage: String(p.stage ?? ''),
      })
    })
    const offDone = window.runtime.EventsOn('portable:done', (d) => {
      const p = d as { success: boolean; message: string; version: string }
      setDownloading(false)
      if (p.success) {
        setDlMsg(
          `✅ Python ${p.version} 下载完成，已自动加入上方「Python 环境」下拉框，选中即可使用`,
        )
        search()
      } else {
        // 取消下载：明确提示（残缺文件已自动清理，下次重新下载）
        const cancelled = String(p.message).includes('取消')
        setDlMsg(
          cancelled
            ? '⏹ 已取消下载，残缺文件已自动清理，下次点击「开始下载」会重新下载'
            : '❌ 下载失败：' + p.message,
        )
      }
    })
    return () => {
      offLog()
      offProg()
      offDone()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 清理面板：点击展开/收起，展开时刷新列表
  const openClean = async () => {
    const next = !showClean
    setShowClean(next)
    setCleanMsg('')
    if (next) {
      try {
        // 防御：后端方法缺失（异常/旧版本）时不崩溃
        if (typeof window.go.main.App.GetPortablePythons !== 'function') {
          setCleanList([])
          setCleanMsg('当前版本不支持清理功能，请更新软件')
          return
        }
        const list = await window.go.main.App.GetPortablePythons()
        setCleanList(Array.isArray(list) ? list : [])
      } catch (e) {
        setCleanList([])
        setCleanMsg('加载失败：' + String(e))
      }
    }
  }

  // 删除某个便携版（占用时后端返回中文提示，就地显示，不弹窗）
  const delPortable = async (version: string) => {
    setDeleting(version)
    setCleanMsg('')
    try {
      await window.go.main.App.DeletePortablePython(version)
      setCleanList((l) => l.filter((x) => x.version !== version))
      setCleanMsg(`已删除 Python ${version}（含其安装的依赖）`)
      search()
    } catch (e) {
      setCleanMsg('删除失败：' + String(e))
    } finally {
      setDeleting('')
    }
  }

  const onPick = async (v: string) => {
    if (v === CUSTOM) {
      try {
        const env = await window.go.main.App.SelectCustomPython()
        setEnvs((prev) => (prev.some((e) => e.path === env.path) ? prev : [env, ...prev]))
        setSelected(env)
      } catch (e) {
        setMsg(String(e))
      }
      return
    }
    setSelected(envs.find((e) => e.path === v) ?? null)
  }

  // 可用作创建虚拟环境的物理环境（虚拟环境不能再套虚拟环境；便携版可作基础环境）
  const physical = envs.filter((e) => !e.isVirtual)

  // 创建虚拟环境
  const createVenv = async () => {
    if (!basePython) {
      setCreateMsg('请先选择物理 Python 环境')
      return
    }
    if (!venvDir.trim() || !venvName.trim()) {
      setCreateMsg('请填写存放路径和环境名称')
      return
    }
    // 拼接完整路径：目录 + 名称
    const full = venvDir.trim().replace(/[\\/]+$/, '') + '\\' + venvName.trim()
    setCreating(true)
    setCreateMsg('')
    try {
      const env = await window.go.main.App.CreateVenv(basePython, full)
      setEnvs((prev) => (prev.some((e) => e.path === env.path) ? prev : [env, ...prev]))
      setSelected(env)
      setCreateMsg('虚拟环境创建成功，已自动选中')
    } catch (e) {
      setCreateMsg('创建失败: ' + String(e))
    } finally {
      setCreating(false)
    }
  }

  // 选择虚拟环境存放目录（复用自定义根目录选择器）
  const browseVenvDir = async () => {
    const dir = await window.go.main.App.SelectCustomRoot()
    if (dir) setVenvDir(dir)
  }

  return (
    <div className="space-y-5">
      <Card
        title="环境配置"
        desc="可指定一个根目录扫描其中的 Python 虚拟环境，也可以搜索系统环境变量中的 Python；没有 Python 的电脑可直接下载内置便携版"
      >
        <div className="space-y-4">
          {/* 自定义环境根目录 + 浏览按钮 */}
          <div className="flex gap-3">
            <div className="flex-1">
              <span className="label">自定义环境根目录（可选）</span>
              <input
                className="field"
                placeholder="例如 D:\Projects，将递归扫描其中的虚拟环境"
                value={root}
                onChange={(e) => setRoot(e.target.value)}
              />
            </div>
            <div className="flex items-end">
              <Button variant="ghost" onClick={browseRoot}>
                📁 浏览…
              </Button>
            </div>
          </div>

          {/* Python 环境选择：环境列表 + 自定义 */}
          <div className="flex gap-3">
            <div className="flex-1">
              <Select
                label="Python 环境"
                value={selected?.path ?? ''}
                onChange={(e) => onPick(e.target.value)}
              >
                {envs.map((env) => (
                  <option key={env.path} value={env.path}>
                    {env.name}
                  </option>
                ))}
                {envs.length === 0 && (
                  // 占位选项：保证下拉框至少有两个选项。
                  // 若只有「自定义环境路径」一个 option，浏览器会把它隐式选中，
                  // 用户再点击不会触发 change 事件，导致无法弹出文件选择框。
                  <option value="" disabled>
                    未搜索到环境，请选择下方「手动选择」
                  </option>
                )}
                <option value={CUSTOM}>＋ 自定义环境路径…（选择 python.exe）</option>
              </Select>
            </div>
            <div className="flex items-end">
              <Button variant="primary" onClick={search} disabled={searching}>
                {searching ? '搜索中…' : '🔍 自动搜索'}
              </Button>
            </div>
          </div>

          {msg && <p className="text-sm text-ink-soft">{msg}</p>}
        </div>
      </Card>

      {/* 未检测到 Python 时的下载引导（页面上显示，不弹窗） */}
      {searched && envs.length === 0 && (
        <Card
          title="⚠ 未检测到 Python 环境"
          desc="打包前需要先准备 Python 环境，以下方式任选其一"
        >
          <div className="space-y-4">
            {/* 方法一：使用内置便携版（推荐） */}
            <div className="rounded-field border border-line bg-base/60 p-4">
              <p className="mb-1 text-sm font-medium text-ink">方法一：使用内置便携版（推荐，最省事）</p>
              <p className="mb-1 text-xs text-ink-faint">
                无需安装、不碰系统，由本工具直接下载一个可用的 Python，下载后自动出现在上方「Python 环境」中
              </p>
              <p className="text-xs text-ink-faint">
                点击页面下方「📥 下载便携版 Python」按钮即可开始
              </p>
            </div>

            {/* 方法二：直链下载 */}
            <div className="rounded-field border border-line bg-base/60 p-4">
              <p className="mb-1 text-sm font-medium text-ink">方法二：直链下载 3.12.10</p>
              <p className="mb-3 text-xs text-ink-faint">
                复制 Windows 64位 · Python 3.12.10 安装包下载链接，可粘贴到浏览器或迅雷下载
              </p>
              <Button variant="ghost" size="sm" onClick={() => copy('direct')}>
                {copied === 'direct' ? '✓ 已复制' : '📋 复制下载链接'}
              </Button>
            </div>

            {/* 方法三：访问 Python 官网 */}
            <div className="rounded-field border border-line bg-base/60 p-4">
              <p className="mb-1 text-sm font-medium text-ink">方法三：访问 Python 官网</p>
              <p className="mb-3 text-xs text-ink-faint">
                前往官网下载 Windows 安装包：<span className="break-all">{PY_DOWNLOAD_URL}</span>
              </p>
              <Button variant="ghost" size="sm" onClick={() => copy('url')}>
                {copied === 'url' ? '✓ 已复制' : '📋 复制地址'}
              </Button>
            </div>

            <div className="flex items-center justify-between">
              <span className="text-xs text-ink-faint">安装完成后，点击下方按钮重新搜索</span>
              <Button variant="ghost" size="sm" onClick={search} disabled={searching}>
                {searching ? '搜索中…' : '🔍 重新搜索'}
              </Button>
            </div>
          </div>
        </Card>
      )}

      {/* 下载便携版 + 新建虚拟环境 + 清理便携版：三个按钮并排，面板均默认收起 */}
      <div className="flex flex-wrap justify-start gap-3">
        <Button variant="ghost" onClick={openDl} disabled={downloading}>
          {showDl ? '▾ 收起下载面板' : '📥 下载便携版 Python'}
        </Button>
        <Button variant="ghost" onClick={() => setShowCreate((v) => !v)}>
          {showCreate ? '▾ 收起新建虚拟环境' : '✨ 新建虚拟环境'}
        </Button>
        <Button variant="ghost" onClick={openClean}>
          {showClean ? '▾ 收起清理 Python 环境' : '🧹 清理 Python 环境'}
        </Button>
      </div>

      {/* 便携版下载面板：点击「下载便携版 Python」后在按钮下方就近展开（东西与原来弹窗一致） */}
      {showDl && (
        <Card title="下载内置便携版 Python" desc="选择版本后点击「开始下载」，进度与日志直接显示在本面板中">
          <div className="space-y-4">
            {/* 版本选择 */}
            <div className="flex gap-3">
              <div className="flex-1">
                <Select
                  label="选择版本"
                  value={dlVersion}
                  onChange={(e) => setDlVersion(e.target.value)}
                  disabled={downloading}
                >
                  <option value="" disabled>
                    请选择版本
                  </option>
                  {dlVersions.map((v) => (
                    <option key={v.version} value={v.version}>
                      Python {v.label}（{v.version}，约 {v.sizeMB} MB）
                      {v.default ? ' — 推荐' : ''}
                    </option>
                  ))}
                </Select>
              </div>
            </div>

            {/* 详细说明 */}
            <div className="rounded-panel border border-line bg-base/60 p-4 text-xs leading-relaxed text-ink-soft">
              <p className="mb-1 font-medium text-ink">该怎么选版本？</p>
              <p>
                默认选择 <b>Python 3.12</b> 即可（最稳定、兼容性最好，绝大多数项目都能用）。
                如果项目代码比较老、依赖的库只支持旧版本，可对应选 3.10 或 3.11；
                3.13 / 3.14 较新，一般用不到。
              </p>
              <p className="mb-1 mt-3 font-medium text-ink">下载后怎么使用？</p>
              <p>
                下载完成后会自动出现在上方「Python 环境」下拉框中（带“内置便携版”标识），
                选中后即可像普通 Python 一样：创建虚拟环境 → 分析安装依赖 → 一键打包。
                它存放在 <span className="break-all">{portableRoot}</span>
                ，不会改动系统里的 Python；不需要时可到「清理 Python 环境」中整版删除。
              </p>
            </div>

            {/* 下载提醒：仅在点击下载后显示（代理访问 GitHub，连接可能较慢） */}
            {downloading && (
              <p className="rounded-panel border border-warn/40 bg-warn/15 px-3 py-2 text-xs leading-relaxed text-warn">
                ⚠ 下载时正在通过国内代理访问 GitHub 获取文件，建立连接预计需要一分钟左右，请耐心等待；
                如果长时间没有进度，工具会自动切换其他镜像重试。
              </p>
            )}

            {/* 下载进度 */}
            {dlProgress && (
              <div className="space-y-2">
                <div className="flex items-center justify-between text-sm">
                  <span className="text-ink">{dlProgress.stage}</span>
                  <span className="font-mono text-ink-soft">
                    {dlProgress.percent}%
                    {dlProgress.speedMBps > 0 && ` · ${dlProgress.speedMBps.toFixed(1)} MB/s`}
                    {dlProgress.totalMB > 0 &&
                      ` · ${dlProgress.doneMB.toFixed(1)} / ${dlProgress.totalMB.toFixed(1)} MB`}
                  </span>
                </div>
                <div className="h-4 w-full overflow-hidden rounded-full bg-line">
                  <div
                    className="progress-animated h-full rounded-full bg-good transition-all duration-300"
                    style={{ width: `${Math.min(100, Math.max(0, dlProgress.percent))}%` }}
                  />
                </div>
                {dlLogs.length > 0 && (
                  <div className="max-h-28 overflow-auto rounded-panel border border-line bg-base px-3 py-2 font-mono text-xs leading-relaxed text-ink-soft">
                    {dlLogs.map((l, i) => (
                      <div key={i}>{l}</div>
                    ))}
                  </div>
                )}
              </div>
            )}

            {dlMsg && <p className="text-sm text-ink-soft">{dlMsg}</p>}

            <div className="flex justify-end gap-3">
              {downloading ? (
                <Button variant="danger" onClick={cancelDl}>
                  ⏹ 取消下载
                </Button>
              ) : (
                <Button variant="primary" onClick={startDl} disabled={!dlVersion}>
                  📥 开始下载
                </Button>
              )}
            </div>
          </div>
        </Card>
      )}

      {/* 清理便携版面板：显示已下载的版本、路径、占用大小与删除按钮 */}
      {showClean && (
        <Card
          title="清理 Python 环境"
          desc="这里只显示工具下载的内置便携版（不含系统安装的 Python）。删除会把该版本的整个文件夹一起删掉，包括已安装的依赖"
        >
          <p className="mb-3 rounded-panel border border-warn/40 bg-warn/15 px-3 py-2 text-xs leading-relaxed text-warn">
            ⚠ 删除前请确认：是否有虚拟环境基于该便携版创建（删除后这些虚拟环境将无法使用）。
            如需恢复，重新下载同版本便携版到原路径即可自动复原，之前安装的依赖包也一并恢复。
          </p>
          {cleanList.length === 0 ? (
            <p className="rounded-panel border border-dashed border-line p-6 text-center text-xs text-ink-faint">
              还没有下载过便携版 Python，点击上方「📥 下载便携版 Python」获取
            </p>
          ) : (
            <div className="space-y-3">
              {cleanList.map((p, i) => (
                <div
                  key={p.version || i}
                  className="flex items-center justify-between gap-4 rounded-panel border border-line bg-base/60 px-4 py-3"
                >
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-ink">
                      内置便携版 · Python {p.version || ''}
                      <span className="ml-2 text-xs text-ink-faint">占用 {p.sizeMB ?? 0} MB</span>
                    </p>
                    <p className="truncate text-xs text-ink-faint">{p.path || ''}</p>
                  </div>
                  <Button
                    variant="danger"
                    size="sm"
                    disabled={deleting === p.version}
                    onClick={() => delPortable(p.version)}
                  >
                    {deleting === p.version ? '⏳ 删除中…' : '🗑 删除'}
                  </Button>
                </div>
              ))}
              {cleanMsg && <p className="text-xs text-ink-soft">{cleanMsg}</p>}
            </div>
          )}
        </Card>
      )}

      {showCreate && (
        <Card
          title="新建虚拟环境"
          desc="使用物理 Python 创建独立的虚拟环境（版本与所选物理 Python 相同）"
        >
          {physical.length === 0 ? (
            <p className="rounded-panel border border-dashed border-line p-6 text-center text-xs text-ink-faint">
              未找到可用的物理 Python，请先在上方搜索、手动选择环境，或下载内置便携版
            </p>
          ) : (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <Select
                  label="物理 Python（自动显示版本）"
                  value={basePython}
                  onChange={(e) => setBasePython(e.target.value)}
                >
                  <option value="" disabled>
                    请选择物理 Python
                  </option>
                  {physical.map((env) => (
                    <option key={env.path} value={env.path}>
                      {env.name} · {env.version || '版本未知'}
                    </option>
                  ))}
                </Select>
                <div>
                  <span className="label">环境名称</span>
                  <input
                    className="field"
                    placeholder="如 venv_py"
                    value={venvName}
                    onChange={(e) => setVenvName(e.target.value)}
                  />
                </div>
              </div>

              <div className="flex gap-3">
                <div className="flex-1">
                  <span className="label">存放路径</span>
                  <input
                    className="field"
                    placeholder="建议英文路径，如 D:\venvs"
                    value={venvDir}
                    onChange={(e) => setVenvDir(e.target.value)}
                  />
                </div>
                <div className="flex items-end">
                  <Button variant="ghost" onClick={browseVenvDir}>
                    📁 浏览…
                  </Button>
                </div>
              </div>

              <p className="rounded-panel border border-line bg-base/60 px-3 py-2 text-xs text-ink-faint">
                创建的环境会保留在你选择的路径中（目标文件夹不存在时自动新建）；如果以后不再需要，建议手动删除整个文件夹。
                中文路径和中文环境名称大部分情况可用，个别含底层扩展的依赖包可能不兼容，建议优先使用英文路径和英文环境名称。
              </p>

              {createMsg && <p className="text-xs text-ink-soft">{createMsg}</p>}

              <div className="flex justify-end">
                <Button variant="primary" onClick={createVenv} disabled={creating}>
                  {creating ? '⏳ 创建中…' : '＋ 创建虚拟环境'}
                </Button>
              </div>
            </div>
          )}
        </Card>
      )}

      {selected && (
        <Card title="当前环境">
          <div className="mb-3 flex items-center justify-between gap-3">
            <span className="min-w-0 flex-1 truncate text-base font-medium text-ink" title={selected.name}>
              {selected.name}
            </span>
            <div className="flex shrink-0 gap-2">
              {selected.isPortable && <Badge tone="accent">内置便携版</Badge>}
              {selected.isVirtual && <Badge tone="accent">虚拟环境</Badge>}
            </div>
          </div>
          <dl className="grid grid-cols-4 gap-4">
            <div>
              <dt className="text-sm text-ink-faint">版本</dt>
              <dd className="mt-1 text-base text-ink">{selected.version || '未知'}</dd>
            </div>
            <div>
              <dt className="text-sm text-ink-faint">pip</dt>
              <dd className="mt-1">
                {selected.hasPip ? <Badge tone="good">可用</Badge> : <Badge tone="bad">缺失</Badge>}
              </dd>
            </div>
            <div>
              <dt className="text-sm text-ink-faint">PyInstaller</dt>
              <dd className="mt-1">
                {selected.hasPyInstaller ? (
                  <Badge tone="good">已安装</Badge>
                ) : (
                  <Badge tone="warn">未安装</Badge>
                )}
              </dd>
            </div>
            <div>
              <dt className="text-sm text-ink-faint">PyArmor（代码加密）</dt>
              <dd className="mt-1">
                {selected.hasPyarmor ? (
                  <Badge tone="good">已安装</Badge>
                ) : (
                  <Badge tone="warn">未安装</Badge>
                )}
              </dd>
            </div>
          </dl>
          <div className="mt-3 flex items-center justify-between gap-3">
            <p className="truncate text-sm text-ink-faint">路径：{selected.path}</p>
            {(!selected.hasPyInstaller || !selected.hasPyarmor) && (
              <span className="shrink-0 text-xs text-ink-faint">
                PyInstaller / PyArmor 未安装时会在分析、打包过程中自动补装，无需手动操作
              </span>
            )}
          </div>
        </Card>
      )}
    </div>
  )
}

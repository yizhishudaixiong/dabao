/// <reference types="vite/client" />

// 与 Go 后端 config 包对齐的类型
export interface PythonEnv {
  name: string
  path: string
  version: string
  isVirtual: boolean
  hasPip: boolean
  hasPyInstaller: boolean
  hasPyarmor: boolean
  isPortable: boolean
  root: string
}

// 内置便携版 Python（与 Go internal/portable 对齐）
export interface PortableVersionInfo {
  version: string
  label: string
  sizeMB: number
  default: boolean
}

export interface PortablePython {
  version: string
  path: string
  dir: string
  sizeMB: number
}

export interface ResourceItem {
  path: string
  enabled: boolean
  isDir: boolean
}

export interface PyarmorInfo {
  installed: boolean
  version: string
}

export interface BuildConfig {
  name: string
  version: string
  author: string
  iconPath: string
  entryScript: string
  oneFile: boolean
  noConsole: boolean
  liteMode: boolean
  encryptMode: string
  resources: ResourceItem[]
  outputDir: string
  contentsDir: string
  hiddenImports: string[]
  useUPX: boolean
  excludeModules: string[]
  pythonPath: string
  // pip 下载源（空=官方源），自动补装 PyInstaller / PyArmor 时复用
  pipMirror: string
  extraArgs: string[]
  cacheAccel: boolean
  uacAdmin: boolean
}

export interface DepInfo {
  importName: string
  pkgName: string
  version: string
  installedVersion: string
  installed: boolean
  required: boolean
  // 引用来源（相对路径:行号），如 "main.py:5, ui.py:12"
  source: string
}

export interface DepsResult {
  missing: DepInfo[]
  resolved: DepInfo[]
  // PyInstaller 钩子补充的间接依赖（已装，随主包自动安装）
  hookExtra: DepInfo[]
  // 官方分析引擎收集的第三方模块全名（加密打包收集用）
  analysisModules: string[]
  // 本次分析耗时（秒）
  analysisSeconds: number
  // 是否命中分析引擎缓存
  isCached: boolean
}

export interface ResourceDep {
  ref: string
  diskPath: string
  exists: boolean
  included: boolean
  kind: string
  sourceFile: string
  line: number
  code: string
}

export interface BuildDonePayload {
  success: boolean
  message: string
  artifacts: string[]
}

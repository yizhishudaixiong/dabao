import type {
  BuildConfig,
  BuildDonePayload,
  DepsResult,
  PortablePython,
  PortableVersionInfo,
  PyarmorInfo,
  PythonEnv,
  ResourceDep,
  ResourceItem,
} from './types/models'

// Wails 运行时注入的 window.go / window.runtime 类型声明
declare global {
  interface Window {
    go: {
      main: {
        App: {
          SearchEnvironments(customRoot: string): Promise<PythonEnv[]>
          SelectCustomPython(): Promise<PythonEnv>
          SelectEntryScript(): Promise<string>
          SelectIcon(): Promise<string>
          SelectOutputDir(): Promise<string>
          SelectCustomRoot(): Promise<string>
          SelectResourceDir(): Promise<string>
          SelectResourceFile(): Promise<string>
          GetVersion(): Promise<string>
          CreateVenv(basePython: string, venvPath: string): Promise<PythonEnv>
          DetectPythonPath(path: string): Promise<PythonEnv>
          AnalyzeDeps(entryScript: string, pythonPath: string, encryptMode: string, mirror: string): Promise<DepsResult>
          InstallDeps(pythonPath: string, pkgs: string[], mirror: string): Promise<void>
          Build(cfg: BuildConfig): Promise<void>
          CancelBuild(): Promise<void>
          CancelDepsInstall(): Promise<void>
          ConfirmExit(): Promise<void>
          ScanResources(dir: string): Promise<ResourceItem[]>
          AddResource(path: string): Promise<ResourceItem>
          AnalyzeResourceDeps(pythonPath: string, entryScript: string, resources: ResourceItem[]): Promise<ResourceDep[]>
          SaveConfigToFile(cfg: BuildConfig): Promise<string>
          LoadConfigFromFile(): Promise<BuildConfig>
          SaveAutoConfig(cfg: BuildConfig): Promise<void>
          LoadAutoConfig(): Promise<BuildConfig>
          DeleteAutoConfig(): Promise<void>
          PreviewCommand(cfg: BuildConfig): Promise<string>
          // 内置便携版 Python
          GetPortableVersions(): Promise<PortableVersionInfo[]>
          GetPortableRootDir(): Promise<string>
          GetPortablePythons(): Promise<PortablePython[]>
          DownloadPortablePython(version: string): Promise<void>
          CancelPortableDownload(): Promise<void>
          DeletePortablePython(version: string): Promise<void>
        }
      }
    }
    runtime: {
      EventsOn(event: string, cb: (data: unknown) => void): () => void
      ClipboardSetText(text: string): Promise<boolean>
    }
  }
}

export {}

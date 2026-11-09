# 开发文档

面向**改代码 / 构建 / 发版**的人。只想下载使用的，请看 [README.md](./README.md)。

## 1. 技术栈与架构

| 层 | 技术 |
| --- | --- |
| 桌面外壳 | Go 1.25+ / Wails v2.15（WebView2） |
| 前端 | React 18 + TypeScript + Tailwind CSS 3（Vite 构建） |
| 打包引擎 | PyInstaller（调用用户所选 Python 环境里的那一份） |
| 代码加密 | PyArmor（CLI，安装在同一环境） |
| 压缩 | 内置 UPX（`go:embed` 进主程序，用时释放到临时目录） |

运行时是**双进程模型**：Go 主进程持有全部业务逻辑，前端只负责展示；所有耗时任务（打包 / 安装 / 分析 / 下载）都在 Go 的 goroutine 里跑，进度通过 `runtime.EventsEmit` 推给前端，前端从不开子进程。

## 2. 目录结构

```
dabao/
├─ main.go                    Wails 入口：窗口参数、文件拖拽、关闭拦截
├─ app.go                     暴露给前端的 App 对象（全部绑定方法都在这里）
├─ webview2_windows.go        WebView2 Runtime 检测（缺失时原生弹窗）
├─ build.ps1 / 一键打包.bat    本地一键构建（同步版本号 + 混淆编译）
├─ build/windows/             Windows 资源：icon.ico、info.json（exe 文件属性/版本）
├─ frontend/
│  ├─ src/App.tsx             步骤状态机、事件监听、统一忙碌状态
│  ├─ src/pages/              5 个步骤页（环境 / 程序信息 / 打包模式 / 依赖 / 构建）
│  ├─ src/components/         Card、Button、Modal、LogPanel、StepNav
│  ├─ src/lib/                路径工具、日志缓冲 hook
│  ├─ src/styles/theme.css    Tailwind 基础层与组件类（.card/.btn/.field…）
│  └─ wailsjs/                Wails 生成的绑定（勿手改，见第 8 节）
└─ internal/
   ├─ builder/                打包流水线：参数组装、执行、加密、图标、资源、诊断
   ├─ deps/                   依赖分析（PyInstaller 官方引擎）、pip 安装、错误诊断
   │  └─ analyze.py           分析脚本本体（go:embed 进二进制，运行时写到临时文件执行）
   ├─ env/                    环境搜索 / 检测 / 虚拟环境创建
   ├─ portable/               内置便携版 Python：下载、解压、校验、删除
   ├─ errhint/                "原因 + 建议 + 最近输出"诊断框架（规则表驱动）
   ├─ fsx/                    文件系统通用判断：路径归属、虚拟环境标志、跳过目录清单
   ├─ cmdutil/                子进程封装（隐藏窗口 + 强制 UTF-8）
   ├─ paths/                  数据目录约定（%LOCALAPPDATA%\dabao、%TEMP%\PythonPackTool）
   └─ version/                版本号唯一来源
```

## 3. 模块职责

| 模块 | 关键文件 | 说明 |
| --- | --- | --- |
| 入口 | `main.go` / `app.go` | 窗口配置、拖拽开关、忙碌时拦截关闭；`App` 上的方法即前端可调用的 API |
| 参数组装 | `builder/command.go` | 把 `BuildConfig` 翻译成 PyInstaller 参数；图标转换、版本信息文件、`--copy-metadata`、本地模块 `--hidden-import`（加密时递归收集子包）、避开命令行长度上限 |
| 执行 | `builder/run.go` | 临时工作区 → 执行 → 收集产物 → 清理；流式转发日志、支持取消 |
| 加密 | `builder/encrypt.go` | 逐文件 PyArmor 加密，超限文件明文兜底，收集 runtime |
| 资源 | `builder/resource.go`、`resdep.go`、`ast_scan.py` | 扫描可打包资源；AST 静态分析代码里引用了哪些资源 |
| 依赖 | `deps/scan.go`、`install.go` | 内嵌 Python 脚本调用 PyInstaller `Analysis` 做分析，与 pip 安装 |
| 环境 | `env/search.go`、`venv.go` | 分层搜索、检测（一次子进程拿全信息）、建虚拟环境 |
| 便携版 | `portable/portable.go` | 国内代理 + 4 线程分块 + 断点续传 + 解压校验 |
| 诊断 | `errhint/` + 三张规则表 | 打包 / pip / 分析三类失败，统一输出中文原因与建议 |
| 通用判断 | `fsx/` | 路径归属（`IsWithin`）、虚拟环境标志（`HasPyvenvCfg`）、跳过目录清单（`IsNoiseDir` / `IsSystemJunkDir`） |

### 常见扩展点

**加一条错误诊断规则**：在 `builder/explain.go` 的 `buildRules`（或 `deps/explain.go` 的 `pipRules` / `analyzeRules`）里按优先级插入一条 `{Keywords: []string{"..."}, Reason: "...", Advice: "..."}`。关键词会先转小写再子串匹配；需要提取细节（如模块名）时用 `Regex`，并在 `Reason` 里用 `{1}` 占位。

**调整扫描要跳过的目录**：只改 `internal/fsx/fsx.go` 里的 `noiseDirs`（项目内扫描）或 `systemJunkDirs`（全盘扫描）。资源分析脚本（`builder/ast_scan.py`）的清单由 Go 侧传入，不需要另改。

**加一个可下载的便携版 Python**：改 `internal/portable/portable.go` 顶部的 `releaseTag`（python-build-standalone 的 release 标签，格式如 `20260901`）与 `Versions()` 列表；`SizeMB` 只用于界面展示。

**加一个前端可调用的方法**：在 `app.go` 的 `App` 上加导出方法 → 跑一次 `wails build -obfuscated` 重新生成绑定（见第 8 节）→ 在 `frontend/src/wails.d.ts` 补上类型声明，否则前端 TS 编译不过。

## 4. 环境准备

```bash
# 必需
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0

# 混淆构建需要（build.ps1 会检查，缺了直接报错退出）
go install mvdan.cc/garble@v0.18.0
```

- **Go 1.25+**（CI 固定 1.27.2）
- **Node 20+**：不必手动装，`build.ps1` 检测不到 npm 时会从 npmmirror 下便携版到 `%LOCALAPPDATA%\dabao\runtime\node`
- 前端依赖：`cd frontend && npm install`

## 5. 构建

**推荐：双击 `一键打包.bat`**，它调用 `build.ps1`，依次做四件事：

1. 准备 Node（缺失时下载便携版）
2. 从 `internal/version/version.go` 读版本号，写进 `build/windows/info.json`（四段式 `x.y.z.0`）
3. 检查 garble
4. `wails build -trimpath -ldflags "-s -w" -obfuscated -garbleargs "-tiny -seed=random"`

产物：`build/bin/Python打包工具.exe`（自包含，可直接分发）。

前端是通过 `main.go` 里的 `//go:embed all:frontend/dist` 打进二进制的，所以 `wails build` 会先执行 `wails.json` 里配置的 `npm install` + `npm run build`；改了前端必须重新构建，只跑 `go build` 拿到的是旧界面。

手动构建（注意混淆相关参数，见第 8 节）：

```powershell
wails build -trimpath -ldflags "-s -w" -obfuscated -garbleargs "-tiny -seed=random"
```

开发调试：`wails dev`（Vite 热更新，端口固定 5173）。

## 6. 版本号与发布

**版本号只有一个来源**：`internal/version/version.go` 的 `Version` 常量。

改完版本号 → 提交 → `git push` 到码云 `master`，剩下全自动：

```
push 源码到码云
   ├─→ 码云 Gitee Go（.workflow/构建成品.yml）
   │      编译混淆版 exe → 打 Tag vX.Y.Z → 创建 Release → 上传 exe
   └─→ 码云「仓库镜像」自动同步代码与 Tag 到 GitHub
           └─→ GitHub Actions（.github/workflows/release.yml，v* Tag 触发）
                  编译 → 发布到 GitHub Releases
```

两条流水线都会在构建前**把版本号同步进 `build/windows/info.json`**（`file_version` / `product_version` / `FileVersion` / `ProductVersion`）。漏掉这一步的话，exe 属性里显示的还是旧版本号——这个坑踩过，现在由脚本兜住。

流水线依赖（已一次性配置）：码云 `RELEASE_TOKEN` 变量、码云仓库镜像的 GitHub 私人令牌（含 `Contents`、`Workflows` 权限）。

## 7. 关键设计取舍

写代码前先看这几条，避免把已有的坑重新挖一遍：

- **环境搜索不做跨次缓存**。Python 环境会随时变（新装、卸载、移动），缓存一旦过期就会给出错误结果。因此分两层：`Search`（只查启动器/PATH/常见目录/conda，不遍历磁盘）与 `SearchDeep`（实时满盘扫描，只在用户点「深度扫描」时触发）。启动时也不搜索，只按记忆路径 `Detect` 一次。
- **全盘扫描深度 7、顶层 6 路并发、每目录只 `ReadDir` 一次**。深度 7 是为了覆盖 Windows 默认的 `Users\你\AppData\Local\Programs\Python\Python3xx`（正好第 7 层）；再深收益极低、耗时翻倍。
- **单文件模式有 4GB 上限**（PyInstaller 归档限制），超了只能改多文件。
- **PyArmor 试用版限制单代码对象 32KB**。超限文件跳过加密、明文打包并在日志里列出；入口脚本超限直接报错。失败归因只在 pyarmor 输出 `out of license` 时才按超限处理，其它错误原样上报。
- **加密后必须把项目内所有本地模块补成 `--hidden-import`（递归，含子包）**。PyArmor 加密后 PyInstaller 读不到任何 import，导入图在每个加密模块处都断了；只补顶层模块时 `from pkg import widgets` 会在产物里报 `ImportError: cannot import name 'widgets'`。这条有端到端回归测试兜着（见第 10 节）。
- **依赖分析缓存靠「环境 + 源码」双指纹失效**。PyInstaller 的 `Analysis-00.toc` 只按已收集文件的 mtime 判断能否复用，两类变化它都看不见：新装的包不在旧清单里、`sys.path` 注入目录里的文件不在依赖图里。因此每次分析前算一个戳（已装包的 `名称==版本` 哈希 + 项目全部 `.py` 的 `路径:大小:mtime` 哈希），与上次不同就删掉 `Analysis-00.toc` 强制重新分析；完全没变仍然 0.9 秒命中。
- **「本地依赖」按文件系统判定，不看 PyInstaller 收集结果**。`sys.path.append('lib')` 之后的 `import helpers`，PyInstaller 根本看不到（`lib` 不在 pathex 里），只看收集结果就会把 `helpers` 报成"缺失的 pip 包"，用户一装就装进同名野包。现在只要项目目录里存在同名 `.py` 就算本地模块。扫描范围同时扩到项目下**全部** `.py`（跳过 `tests/`、`docs/`、`examples/`、`setup.py`），否则注入目录里的真实依赖会整片漏掉。
- **能导入但没有任何发行版声明提供的模块，不算依赖**。`_yaml`、`win32ctypes` 这类某个包的内部模块/编译扩展本身没有独立发行版，报成依赖只会让用户装到同名野包；反之"导入不了又查不到映射"才是真缺失，用模块名当包名作最佳猜测。`try: import x / except ImportError` 里的**可选依赖**缺失时程序自带降级，同样不报。
- **模块名 → pip 包名：已装环境的映射优先于静态表**。`importlib.metadata.packages_distributions()` 是解释器自己给出的权威答案（还能对上 `opencv-contrib-python` 这类变体），静态表（pipreqs 0.5.0 + 手写补充）只在"模块尚未安装"时作为唯一线索。静态表里的过期映射（`MySQLdb → MySQL-python` 只支持 Python 2）因此会被自动绕过。
- **资源分析对"候选"宽松、对"结论"严格**。候选阶段**扫描任意位置的字符串字面量**（赋值、字典/列表、函数参数与默认值都算）——`QIcon('icon.png')`、`resource_path('data/config.json')`、`MENU=[{'icon':'a.png'}]` 这类写法函数名不可枚举、位置也不固定，按白名单必然漏。排除三类噪音后交给结论阶段：文档字符串、f-string 片段（`f'{BASE}/a.png'` 的常量段单独解析会得到错路径）、带空格的提示文案。结论阶段的硬规则：写入模式的 `open` 不算资源；Qt 的 `:/icons/a.png` 与 URL、glob 不是磁盘资源；既不在磁盘上、没被勾选、又没有任何目录分隔符的裸名字丢弃；带目录的 `assets/missing.png` 保留（"引用了但文件不存在"是真实问题）。
- **构建成功后检查产物缺不缺模块**（`builder/missingmods.go`）。PyInstaller 缺模块时**构建仍算成功**，只把清单写进 `build/<name>/warn-<name>.txt`，产物一启动就 `ModuleNotFoundError`。该文件里绝大多数是平台条件模块（`pwd`/`grp`/`fcntl`）和第三方库的可选导入，所以只挑「导入方是用户自己的代码、且不是 `optional`」的条目，在日志末尾提示一次。判"导入方是不是用户代码"时，入口脚本在文件里是完整路径、其它模块只有模块名，两种都要处理。
- **`--add-data` 的 DEST 是「目标目录」，不是文件路径**（`builder.resourceTarget`）。PyInstaller 会把文件名追加到 DEST 后面（`format_binaries_and_datas` 的文件分支），所以单文件必须传它**所在的目录**：传 `assets/logo.png` 会让产物里出现 `assets/logo.png/logo.png`，程序按 `sys._MEIPASS` 拼原路径时打开的是个目录，直接 `FileNotFoundError`。有端到端用例 `TestResourcePackedPaths` 与单测 `TestResourceTarget` 锁住这条语义。
- **资源结果按"该不该处理"排序**：未包含且文件存在 → 未包含且不存在 → 已包含。界面按这个顺序展示，最该看的在最上面。
- **打包命令行有长度上限**（Windows `CreateProcess` 32767 字符）。装了几百个包时 `--copy-metadata` + `--collect-all` + `--hidden-import` 很容易逼近上限，超了只会得到一句 `filename or extension is too long`。`fitCmdline` 在超长时优先丢弃 `--copy-metadata`（最不影响产物），仍超长则给出中文报错，绝不擅自删 hidden-import。
- **UPX 会提高杀软误报率**，默认关闭。
- **便携版下载没有校验和**（上游未提供 SHA256），只做总长度校验；下载走第三方 GitHub 代理，属于已知信任边界。
- **前端日志上限 4000 行**、面板只渲染最近 800 行；超出部分丢弃（诊断只看末尾）。

## 8. Wails 绑定生成（重要）

`frontend/wailsjs/` 是**生成文件**，但如果生成方式不对，混淆版会直接崩。

- 混淆构建里，绑定是 `ObfuscatedCall(id, [args])`，`id` 是方法名在**字典序**中的下标（Wails 用反射取方法，Go 保证按名字排序）。
- 因此**新增/删除/重命名任何 `App` 方法，都会让它之后的所有 id 顺移**，旧文件必须重新生成。
- **不要用 `wails generate module`**：它生成的是非混淆版（`window['go'][...]`）。`main.tsx` 在检测到 `window.ObfuscatedCall` 时会把这份绑定挂到 `window.go.main.App`，非混淆版再调 `window['go'][...]` 就是自我递归。
- 正确做法：跑一次 `wails build -obfuscated ...`，Wails 会在编译前端之前生成混淆绑定。想只更新绑定，也可以先随便跑一次混淆构建再中断。
- 改完可以用 `git diff frontend/wailsjs/go/main/App.js` 自查：正常只会看到新方法一行 + 后续 id 顺移。

## 9. 代码约定

这些规则都是踩坑后总结的，新增代码请对齐：

1. **子进程一律走 `cmdutil.Command`**：隐藏窗口（不闪黑框）+ 强制 `PYTHONIOENCODING=utf-8`。
2. **用 `StdoutPipe` 时先读完输出、再 `cmd.Wait()`**。`Wait` 会关闭管道，反过来会把末尾几行吞掉——而报错信息恰好都在末尾。
3. **大文件禁止 `os.ReadFile` 整读**，用 `io.Copy`（单文件产物可达 GB 级）。
4. **错误提示走 `errhint`**：新增一条规则就加到对应规则表，别在业务代码里散写中文提示。
5. **路径归属与"跳过哪些目录"统一用 `internal/fsx`**，不要各写一份：判断子路径用 `fsx.IsWithin`（不能用字符串前缀），识别虚拟环境用 `fsx.HasPyvenvCfg`，扫描跳过用 `IsNoiseDir` / `IsSystemJunkDir`。删除 / 写出路径前必须校验归属（`Workspace.Cleanup`、`portable.Delete`、`validateAppName`）。
6. **前端日志用 `lib/useLogs.ts` 的 `useLogBuffer`**，不要 `setState(prev => [...prev, line])`：那是 O(n²) 拷贝 + 每行一次整页重渲染。
7. **界面文案必须与后端实际行为一致**（例如加密档位说明只能写实际启用的 `--enable-jit`，不能把没启用的 restrict 选项写上去）。
8. **注释写"为什么"**，尤其是反直觉的地方（比如"为什么这里不能缓存""为什么先读后 Wait"）。

## 10. 测试

```bash
go test ./...                 # 单元测试
cd frontend && npx tsc --noEmit && npm run build
```

现有覆盖面：

| 包 | 覆盖点 |
| --- | --- |
| `internal/env` | 全盘扫描语义：venv 识别、跳过名单、深度边界、符号链接 |
| `internal/portable` | 分块覆盖完整性（含不能被 4 整除的大小）、端到端下载、断点续传、删除时的路径校验 |
| `internal/builder` | 程序名称校验、加密前的 .py 收集规则（`environment/` 不被误跳）、加密时的本地模块递归收集、排除模块的生效范围、超长命令行裁剪、资源扫描准确率（GBK 源码、pathlib `/`、误报/漏报用例） |
| `internal/fsx` | 路径归属、虚拟环境标志识别、跳过目录清单 |
| `pypacker`（根包） | 路径归属判断、便携版标记 |
| `internal/deps` | 错误诊断规则匹配、依赖分类准确率（本地模块/可选依赖/测试目录/包名映射） |

需要真实打包 / 联网的端到端测试默认跳过，改动加密或分析链路时手动跑一遍：

```bash
# 加密打包：本地子包 + 包内相对导入 + 二级嵌套（约 35 秒，需已装 pyinstaller / pyarmor）
DABAO_E2E=1 go test ./internal/builder/ -run TestEncryptedBuildLocalSubpackage -v

# 依赖分析缓存：装包后重新分析必须拿到新清单（约 60 秒，会建 venv 并联网装包）
DABAO_E2E=1 go test ./internal/deps/ -run TestInstallThenReanalyze -v
```

两者都可用 `DABAO_PYTHON` 指定解释器路径。

**手工验证清单**（改动涉及打包链路时必做）：

1. 用最小脚本（`print("hi")`）跑一遍单文件 + 无控制台，双击产物确认能运行
2. 勾选一个资源文件夹，确认产物里路径与界面「打包后」显示一致
3. 选「基础加密」打包，确认产物能运行、日志里没有"未加密"警告；**项目里只要有子包（`pkg/mod.py`），就必须实际跑一遍产物**——加密后子模块没被收集是这条链路上最容易复发的坑
4. 下载一个便携版 Python，确认列表出现、能选中并打包
5. 触发一次失败（例如入口脚本写错语法），确认日志底部有中文原因 + 建议 + 最近输出

## 11. 常见构建问题

| 现象 | 原因与处理 |
| --- | --- |
| `未找到 garble` | `go install mvdan.cc/garble@v0.18.0`，并确认 `%GOPATH%\bin` 在 PATH 里 |
| `pattern all:frontend/dist: no matching files` | 前端没构建。`cd frontend && npm run build`，或直接跑 `wails build`（会自动构建） |
| 混淆版启动白屏 / 点击无反应 | 绑定被 `wails generate module` 覆盖成了非混淆版，重新跑一次 `wails build -obfuscated` |
| 构建被杀软拦截 | 把项目目录和 `%TEMP%\PythonPackTool` 加入白名单 |
| `wails dev` 起不来 | 5173 端口被占用；Vite 配置了 `strictPort` |

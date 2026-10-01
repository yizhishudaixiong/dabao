# Python打包工具（🐨 一只树袋熊出品）

> **产品信息（构建规范）**
> - 产品名称：Python打包工具
> - 作者：一只树袋熊
> - 版本号：读取程序内部版本号（`internal/version/version.go` 中的唯一常量），界面显示与构建产物版本均以此为准，修改版本只需改该常量后运行 `build.ps1`
> - **构建产物规则（每次编译都须遵守）**：
>   1. 文件详细信息中的「文件版本」「产品版本」必须对应程序实际版本（`build/windows/info.json` 四段格式 `x.y.z.0`，由 `build.ps1` 从 `version.go` 自动同步）
>   2. 产品名称必须对应实际名称「Python打包工具」（`info.json` 中 `ProductName`）
>   3. 版权必须对应「一只树袋熊」（`info.json` 中 `CompanyName` 与 `LegalCopyright`）

基于 **Go + Wails v2 + React + Tailwind CSS** 的 Windows 桌面工具，底层调用 **PyInstaller** 完成打包。
界面为**亮色简约风格**（圆角 / 留白 / 阴影），采用分步向导式流程。

## 功能

- **自动搜索 Python 环境**：扫描系统环境变量、常见安装目录、以及用户自定义环境根目录下的虚拟环境；自动**过滤 Windows 应用执行别名等假环境**；虚拟环境在下拉框中显示完整路径；也支持手动选择任意 `python.exe`；无 Python 的电脑可一键下载**内置便携版**直接使用（存于 `%LOCALAPPDATA%\dabao\runtime`，不碰系统）
- **无黑框干扰**：所有子进程（检测 / pip 安装 / PyInstaller 打包）均以隐藏窗口方式后台运行
- **自定义程序信息**：名称、版本、作者、图标（.ico/.png/.jpg 自动转换）
- **打包模式**：单文件 / 多文件、显示 / 隐藏控制台、精简 / 正常打包、程序与资源一起打包、自定义多文件依赖文件夹名称、UPX 压缩
- **代码加密（PyArmor）**：支持「不加密 / 基础加密 / 深度加密」三档，深度档叠加 JIT 动态指令保护；用户选择加密后，依赖分析自动把 PyArmor 纳入安装清单（缺失时强制安装），加密产物随打包生成并整体清理
- **自动依赖分析**：基于 PyInstaller 官方分析引擎（字节码扫描 + 官方钩子）结合 AST 候选提取差集判定，缺失项可一键安装到所选 Python 环境（支持官方/清华/阿里/自定义镜像源）
- **开箱即用**：缺失 PyInstaller / PyArmor 时按需自动安装；缺失 WebView2 Runtime 时原生弹窗引导安装
- **只交付最终产物**：所有中间产物在独立临时工作区中完成，结束后自动整体清理，**绝不触碰用户 Python 环境与已装依赖**

## 目录结构

```
pypacker/
├── main.go                  # 入口：WebView2 检测 + Wails 启动
├── app.go                   # 前后端绑定方法（环境/依赖/打包/自检）
├── webview2_windows.go      # 微软官方 API 检测 WebView2（缺失时原生弹窗）
├── wails.json               # Wails 项目配置
├── build/windows/icon.ico  # 打包进 EXE 的图标（树袋熊）
├── frontend/src/assets/icon.png # 界面 Logo（树袋熊）
├── internal/
│   ├── cmdutil/             # 跨平台子进程封装（Windows 隐藏窗口，防黑框）
│   ├── version/             # 程序内部版本号（唯一版本来源）
│   ├── paths/               # 统一数据目录（%LOCALAPPDATA%\dabao 下各子目录）
│   ├── env/                 # Python 环境搜索与检测（过滤假环境）
│   ├── deps/                # 依赖分析 + 安装
│   ├── builder/             # PyInstaller 命令组装 / 执行 / 产物收集 / 清理 / PyArmor 加密
│   ├── config/              # 前后端共享的数据模型
└── frontend/                # React + TS + Tailwind（亮色主题）
    └── src/
        ├── App.tsx          # 全局状态 + 5 步流程（统一忙碌状态：打包/安装/分析/下载）
        ├── components/      # 通用 UI（卡片/按钮/开关/步骤条/日志面板）
        ├── lib/             # 通用工具（路径解析等）
        └── pages/           # 环境 / 信息 / 模式 / 依赖 / 构建
```

## 在 Windows 上构建

前置环境（一次性）：

1. **Go** 1.25+：https://go.dev/dl/
2. **Node.js** 20+（推荐 24 LTS）：https://nodejs.org/
3. **Wails CLI**：

   ```bat
   go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
   ```

构建（在项目根目录执行，一键脚本会自动同步版本号再编译）：

```powershell
.uild.ps1
```

产物：`build/bin/Python打包工具.exe`（自包含，可直接分发）。

> 开发调试：`wails dev` 可热重载前端。

## 运行期说明

- 打包工具本体为自包含 EXE，**不嵌入 WebView2**（保持体积最小）
- 目标机器若缺少 WebView2 Runtime，启动时会原生弹窗提示安装（Win10/11 一般自带）
- 使用 PyInstaller 打包出的程序在部分杀毒软件下可能出现误报，属常见现象，可对产物进行代码签名缓解

## 技术要点

- 打包命令基于 `python -m PyInstaller`，全程参数化传参，规避中文路径 / 空格问题
- 代码加密：先由所选环境的 `pyarmor` CLI 加密入口脚本（基础档默认；深度档在基础之上加 `--enable-jit` 动态指令保护，均只用免费版功能），再把加密脚本 + `pyarmor_runtime` 运行时目录一并打入产物；PyArmor 免费版不支持 restrict 类选项（`--private` / `--assert-call` 等），故未使用；加密分析均以隐藏窗口运行，不闪黑框
- 打包完整性：自动把入口脚本同目录的本地 `.py` 模块和子包作为 `--hidden-import` 打入产物，并加入 `--paths`，解决 PyInstaller 对中文模块名（如 `配置.py`）import 无法自动收集导致运行报 "No module named" 的问题
- 所有子进程通过 `internal/cmdutil` 统一创建，Windows 下设置 `CREATE_NO_WINDOW`，杜绝黑框闪烁
- 假环境过滤：跳过 WindowsApps 应用执行别名目录、0 字节占位文件，且无法识别版本的环境自动剔除
- 临时工作区位于 `%TEMP%\PythonPackTool\job_<时间戳>`，清理前做路径白名单校验（`isWithin`），只删自己创建的目录
- 依赖分析：PyInstaller 官方 `Analysis` 引擎收集环境真实模块（含本地模块发现），AST 提取入口脚本的 import 候选，差集判定缺失；「已装判定」采用三层匹配（纯模块名 / 目录前缀 / 文件名）+ `importlib.util.find_spec` 权威兜底，与 PyInstaller 命名规则解耦，避免 .pyd 类扩展模块误报；缺失项按所选镜像源（官方/清华/阿里/自定义）安装到目标环境；分析缓存按入口脚本哈希分目录，保留最近 10 个项目自动清理
<img width="1058" height="714" alt="image" src="https://github.com/user-attachments/assets/dd1ecf20-14cd-4205-bd96-22b8cbc1ab08" />
<img width="1063" height="711" alt="image" src="https://github.com/user-attachments/assets/86ba248e-61ad-4030-bfd6-4513e7d93e88" />
<img width="1060" height="707" alt="image" src="https://github.com/user-attachments/assets/56435bed-4580-4768-a5c9-7f392c67486d" />
<img width="1063" height="707" alt="image" src="https://github.com/user-attachments/assets/90184989-d7ea-417f-983c-92c06084a79c" />
<img width="1059" height="704" alt="image" src="https://github.com/user-attachments/assets/85331921-0850-47e7-be27-560f7c30f291" />
<img width="1065" height="704" alt="image" src="https://github.com/user-attachments/assets/ab757b85-d14b-4d05-ab98-992f98df24f8" />

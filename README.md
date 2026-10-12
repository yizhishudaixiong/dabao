# Python打包工具（🐨 一只树袋熊出品）

> 基于 **Go + Wails v2 + React + Tailwind CSS** 的 Windows 桌面工具，底层调用 **PyInstaller** 完成打包。
> 把 Python 脚本一键打包成独立 EXE，支持代码加密与 UPX 压缩。界面为亮色简约风格，采用分步向导式流程。

## 功能特性

- **自动搜索 Python 环境**：扫描系统环境变量、常见安装目录、用户自定义环境根目录下的虚拟环境，并**兜底扫描整个 C 盘**（最多 6 层，跳过系统与缓存大目录）捕捉任意位置的 Python；自动**过滤 Windows 应用执行别名、0 字节占位、豆包沙箱运行时等假环境**；虚拟环境在下拉框中显示完整路径；也支持手动选择任意 `python.exe`；无 Python 的电脑可一键下载**内置便携版**直接使用（存于 `%LOCALAPPDATA%\dabao\runtime`，不碰系统）
- **无黑框干扰**：所有子进程（检测 / pip 安装 / PyInstaller 打包）均以隐藏窗口方式后台运行
- **自定义程序信息**：名称、版本、作者、图标（.ico / .png / .jpg 自动转换）
- **灵活打包模式**：单文件 / 多文件、显示 / 隐藏控制台、精简 / 正常打包、程序与资源一起打包、自定义多文件依赖文件夹名称、UPX 压缩
- **代码加密（PyArmor）**：支持「不加密 / 基础加密 / 深度加密」三档，深度档叠加 JIT 动态指令保护；用户选择加密后，依赖分析自动把 PyArmor 纳入安装清单（缺失时强制安装），加密产物随打包生成并整体清理
- **自动依赖分析**：基于 PyInstaller 官方分析引擎（字节码扫描 + 官方钩子）结合 AST 候选提取差集判定，缺失项可一键安装到所选 Python 环境（支持官方 / 清华 / 阿里 / 自定义镜像源）；分析失败时输出**中文诊断**（原因、建议 + 最近错误输出原文），不再只有干巴巴的英文报错；已排除第三方库内部模块与相对导入误报，虚拟环境（任意命名，如 venv_py）不会被误扫
- **资源依赖分析**：静态扫描入口目录下所有 .py（含子文件夹）的读取调用（open / Path / cv2.imread 等），按 `pyvenv.cfg` 标志自动识别并跳过任意命名的虚拟环境，列出代码引用的资源是否已勾选打包，避免运行时"找不到文件"
- **包元数据自动复制**：打包时自动把所选环境中所有已安装包的元数据（dist-info）复制进产物，streamlit 等"导入时查询自身版本号"的包打包后不再报 `PackageNotFoundError`
- **配置记忆**：可开启自动记忆，程序名、入口脚本、Python 环境、资源清单与勾选状态在下次打开时自动还原（可随时关闭）
- **开箱即用**：缺失 PyInstaller / PyArmor 时自动补装（加密打包前也会自动检查并安装 PyArmor）；缺失 WebView2 Runtime 时原生弹窗引导安装
- **只交付最终产物**：所有中间产物在独立临时工作区中完成，结束后自动整体清理，**绝不触碰用户 Python 环境与已装依赖**

## 界面预览

<img width="1058" height="714" alt="image" src="https://github.com/user-attachments/assets/dd1ecf20-14cd-4205-bd96-22b8cbc1ab08" />
<img width="1063" height="711" alt="image" src="https://github.com/user-attachments/assets/86ba248e-61ad-4030-bfd6-4513e7d93e88" />
<img width="1060" height="707" alt="image" src="https://github.com/user-attachments/assets/56435bed-4580-4768-a5c9-7f392c67486d" />
<img width="1063" height="707" alt="image" src="https://github.com/user-attachments/assets/90184989-d7ea-417f-983c-92c06084a79c" />
<img width="1059" height="704" alt="image" src="https://github.com/user-attachments/assets/85331921-0850-47e7-be27-560f7c30f291" />
<img width="1065" height="704" alt="image" src="https://github.com/user-attachments/assets/59fa3f34-68e3-4eeb-95ed-741f4d3b3e1d" />


## 下载安装

前往仓库 **Releases** 页面下载最新版本（或直接访问 `github.com/yizhishudaixiong/dabao/releases`）。

- 仅支持 **Windows 10 及以上** 版本
- 需要 **WebView2 Runtime**（Win10 / Win11 一般自带，缺失时程序会原生弹窗引导安装）

> 注意：经 UPX 压缩的版本可能被部分杀毒软件误报，属常见现象；不放心可下载未压缩版本或自行编译。

## 使用方法

1. 运行 `Python打包工具.exe`
2. 选择或搜索 Python 环境（无 Python 可一键下载内置便携版）
3. 填写程序名称、版本、作者、图标
4. 选择打包模式与加密等级
5. 点击打包，等待完成，产物自动输出

## 从源码构建

**最简单：双击打包**——双击项目根目录的 `一键打包.bat`，脚本会自动同步版本号、检测/准备 Node.js 并完成混淆构建，结束后按任意键关闭窗口。

前置环境：**Go 1.25+**、**Wails CLI**（Node.js 无需单独安装：脚本检测不到时会自动从国内镜像下载内置便携版）

也可在项目根目录手动执行：

```powershell
.\build.ps1   # 混淆构建（默认，配合 main.tsx 转接层）
```

产物：`build/bin/Python打包工具.exe`（自包含，可直接分发）。

开发调试：`wails dev` 可热重载前端。

## 自动构建发布（云端 CI）

本项目已接入**双平台全自动发布**：只需**修改版本号并 push 到码云**，其余全部自动完成，本地无需再手动打包上传。

```
push 源码到码云
   ├─→ 码云 Gitee Go：自动编译混淆版 exe → 打 Tag → 发布到码云 Releases
   └─→ 码云仓库镜像：自动同步代码与 Tag 到 GitHub
            └─→ GitHub Actions：检测到 v* Tag → 自动编译 → 发布到 GitHub Releases
```

**发布步骤（唯一要做的）**：

1. 修改 `internal/version/version.go` 中的版本号（唯一版本来源，如 `"6.2.0"`）
2. `git push` 到码云 master

**依赖配置（已一次性完成）**：

- 码云 Gitee Go 流水线：`auto-build-release`（push master 触发，编译 + 打 Tag + 发 Release）
- 码云「仓库镜像管理」：Push 方向自动同步到 GitHub
- GitHub Actions 工作流：`.github/workflows/release.yml`（`v*` Tag 触发；含手动触发入口，可在 Actions 页手动 Run workflow 验证）
- GitHub 私人令牌：码云镜像使用（仓库级，含 `Contents`、`Workflows` 读写权限）

## 技术栈

Go · Wails v2 · React · Tailwind CSS · PyInstaller · PyArmor

## 更新日志

### v6.2.0
- **接入云端自动发布**：push 码云后由码云 Gitee Go 自动编译混淆版 exe 并发布到码云 Releases；码云仓库镜像自动同步代码与 Tag 到 GitHub，GitHub Actions 检测到 `v*` Tag 后自动编译并发布到 GitHub Releases，本地无需再手动打包上传

### v6.1.0
- **修复 streamlit 等包打包后报错**：打包时自动复制环境里所有已装包的发行版元数据（`--copy-metadata`），不再因缺 dist-info 报 `PackageNotFoundError`
- **修复依赖检查误报**：排除 site-packages 库文件与相对导入，第三方库内部模块不再被误判为"缺失"（实测缺失项 43 → 0），加密打包的模块清单恢复正常（0 → 196）
- **修复资源依赖分析误报**：按 `pyvenv.cfg` 标志识别并跳过任意命名虚拟环境（venv_py 等），不再把第三方库内部引用当资源（实测 1359 → 4 条）
- **修复空结果崩溃**：后端列表为空时返回 `[]` 而非 `null`，资源分析结果为空不再弹错误页
- **修复记忆功能**：启动还原完成前不重新扫描资源、不自动保存，重开工具不再"全选资源"、也不会用空配置覆盖记忆文件

## 版权

© 一只树袋熊（yizhishudaixiong）

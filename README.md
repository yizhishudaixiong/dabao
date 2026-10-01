# Python打包工具（🐨 一只树袋熊出品）

> 基于 **Go + Wails v2 + React + Tailwind CSS** 的 Windows 桌面工具，底层调用 **PyInstaller** 完成打包。
> 把 Python 脚本一键打包成独立 EXE，支持代码加密与 UPX 压缩。界面为亮色简约风格，采用分步向导式流程。

## 功能特性

- **自动搜索 Python 环境**：扫描系统环境变量、常见安装目录及用户自定义环境根目录下的虚拟环境；自动**过滤 Windows 应用执行别名等假环境**；虚拟环境在下拉框中显示完整路径；也支持手动选择任意 `python.exe`；无 Python 的电脑可一键下载**内置便携版**直接使用（存于 `%LOCALAPPDATA%\dabao\runtime`，不碰系统）
- **无黑框干扰**：所有子进程（检测 / pip 安装 / PyInstaller 打包）均以隐藏窗口方式后台运行
- **自定义程序信息**：名称、版本、作者、图标（.ico / .png / .jpg 自动转换）
- **灵活打包模式**：单文件 / 多文件、显示 / 隐藏控制台、精简 / 正常打包、程序与资源一起打包、自定义多文件依赖文件夹名称、UPX 压缩
- **代码加密（PyArmor）**：支持「不加密 / 基础加密 / 深度加密」三档，深度档叠加 JIT 动态指令保护；用户选择加密后，依赖分析自动把 PyArmor 纳入安装清单（缺失时强制安装），加密产物随打包生成并整体清理
- **自动依赖分析**：基于 PyInstaller 官方分析引擎（字节码扫描 + 官方钩子）结合 AST 候选提取差集判定，缺失项可一键安装到所选 Python 环境（支持官方 / 清华 / 阿里 / 自定义镜像源）
- **开箱即用**：缺失 PyInstaller / PyArmor 时按需自动安装；缺失 WebView2 Runtime 时原生弹窗引导安装
- **只交付最终产物**：所有中间产物在独立临时工作区中完成，结束后自动整体清理，**绝不触碰用户 Python 环境与已装依赖**

## 界面预览

<img width="1058" height="714" alt="image" src="https://github.com/user-attachments/assets/dd1ecf20-14cd-4205-bd96-22b8cbc1ab08" />
<img width="1063" height="711" alt="image" src="https://github.com/user-attachments/assets/86ba248e-61ad-4030-bfd6-4513e7d93e88" />
<img width="1060" height="707" alt="image" src="https://github.com/user-attachments/assets/56435bed-4580-4768-a5c9-7f392c67486d" />
<img width="1063" height="707" alt="image" src="https://github.com/user-attachments/assets/90184989-d7ea-417f-983c-92c06084a79c" />
<img width="1059" height="704" alt="image" src="https://github.com/user-attachments/assets/85331921-0850-47e7-be27-560f7c30f291" />
<img width="1065" height="704" alt="image" src="https://github.com/user-attachments/assets/ab757b85-d14b-4d05-ab98-992f98df24f6" />

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

前置环境：**Go 1.25+**、**Node.js 20+**、**Wails CLI**

在项目根目录执行（一键脚本会自动同步版本号再编译）：

```powershell
.\build.ps1
```

产物：`build/bin/Python打包工具.exe`（自包含，可直接分发）。

开发调试：`wails dev` 可热重载前端。

## 技术栈

Go · Wails v2 · React · Tailwind CSS · PyInstaller · PyArmor

## 版权

© 一只树袋熊（yizhishudaixiong）

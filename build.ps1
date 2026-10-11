# build.ps1 - 一键构建 Python打包工具
# 用法：在项目根目录执行  .\build.ps1
# 功能：1. 从 internal\version\version.go 读取内部版本号（唯一版本来源）
#       2. 把版本号同步到 build\windows\info.json（Windows 四段格式 x.y.z.0）
#       3. 执行 wails build 编译出 build\bin\Python打包工具.exe
# 说明：以后改版本号只需改 version.go 一处，再运行本脚本即可
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

# ---------- 0. 确保 Node.js / npm 可用 ----------
# 机器未安装 Node.js 时，从国内镜像（npmmirror）下载内置便携版到
# %LOCALAPPDATA%\dabao\runtime\node，首次约 30MB，之后直接复用
$nodeRuntime = Join-Path $env:LOCALAPPDATA 'dabao\runtime\node'
function Test-NpmAvailable {
    $n = Get-Command npm -ErrorAction SilentlyContinue
    if ($n) { return $true }
    foreach ($c in @('C:\Program Files\nodejs\npm.cmd', "$env:LOCALAPPDATA\Programs\nodejs\npm.cmd", "$env:APPDATA\npm\npm.cmd")) {
        if (Test-Path $c) { return $true }
    }
    if (Test-Path (Join-Path $nodeRuntime 'npm.cmd')) { return $true }
    return $false
}
if (-not (Test-NpmAvailable)) {
    Write-Host '未检测到系统 Node.js，正在准备内置便携版 Node（首次下载约 30MB）...'
    $zip = Join-Path $env:TEMP 'dabao_node.zip'
    $url = 'https://npmmirror.com/mirrors/node/v20.19.0/node-v20.19.0-win-x64.zip'
    curl.exe -L -o $zip $url
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path $zip)) {
        Write-Host '[错误] 便携 Node 下载失败，请手动安装 Node.js 20+ 后重试' -ForegroundColor Red
        exit 1
    }
    New-Item -ItemType Directory -Force -Path $nodeRuntime | Out-Null
    Expand-Archive -Path $zip -DestinationPath $nodeRuntime -Force
    $inner = Get-ChildItem $nodeRuntime -Directory | Select-Object -First 1
    if ($inner) {
        Get-ChildItem $inner.FullName | Move-Item -Destination $nodeRuntime -Force
        Remove-Item $inner.FullName -Recurse -Force
    }
    Remove-Item $zip -Force -ErrorAction SilentlyContinue
}
if (Test-Path (Join-Path $nodeRuntime 'npm.cmd')) {
    $env:PATH = $nodeRuntime + ';' + $env:PATH
} elseif (-not (Get-Command npm -ErrorAction SilentlyContinue)) {
    Write-Host '[错误] 无法准备 Node.js，请手动安装 Node.js 20+ 后重试' -ForegroundColor Red
    exit 1
}

# ---------- 1. 读取内部版本号 ----------
$verFile = Join-Path $root 'internal\version\version.go'
$match = Select-String -Path $verFile -Pattern 'Version\s*=\s*"([^"]+)"'
if (-not $match) {
    Write-Host '[错误] 无法从 version.go 读取版本号' -ForegroundColor Red
    exit 1
}
$ver = $match.Matches[0].Groups[1].Value
$ver4 = "$ver.0"
Write-Host "内部版本号: $ver    Windows 文件版本: $ver4"

# ---------- 2. 同步 info.json 版本号（保留原格式，无 BOM） ----------
$infoPath = Join-Path $root 'build\windows\info.json'
$info = Get-Content $infoPath -Raw -Encoding UTF8
$replFile = '"file_version": "' + $ver4 + '"'
$replProd = '"product_version": "' + $ver4 + '"'
$info = $info -replace '"file_version":\s*"[^"]*"', $replFile
$info = $info -replace '"product_version":\s*"[^"]*"', $replProd
# 兼容 winres 格式（info.0000 下的 PascalCase 键）
$replFV = '"FileVersion": "' + $ver4 + '"'
$info = $info -replace '"FileVersion":\s*"[^"]*"', $replFV
$replPV = '"ProductVersion": "' + $ver4 + '"'
$info = $info -replace '"ProductVersion":\s*"[^"]*"', $replPV
[System.IO.File]::WriteAllText($infoPath, $info, (New-Object System.Text.UTF8Encoding($false)))
Write-Host "已同步 info.json 版本 -> $ver4"

# ---------- 2.5 确保 garble 可用（wails 混淆编译所需） ----------
$garbleBin = Join-Path (go env GOPATH) 'bin'
if ($env:Path -notlike "*$garbleBin*") {
    $env:Path = "$garbleBin;$env:Path"
}
if (-not (Get-Command garble -ErrorAction SilentlyContinue)) {
    Write-Host '[错误] 未找到 garble，请先执行: go install mvdan.cc/garble@latest' -ForegroundColor Red
    exit 1
}

# ---------- 3. 编译 ----------
Push-Location $root
# 说明：garble/wails 会向 stderr 打印 seed 等普通提示，PowerShell 5.1 会把
# stderr 内容包装成 ErrorRecord 用红色显示（看着像报错，其实不是）。
# 用 cmd /c 包裹并在 cmd 层合并 2>&1，PowerShell 只收到普通文本，不再出现红色提示
cmd /c "wails build -trimpath -ldflags `"-s -w`" -obfuscated -garbleargs `"-tiny -seed=random`" 2>&1"
$ok = $LASTEXITCODE -eq 0
Pop-Location

if ($ok) {
    Write-Host "构建成功: build\bin\Python打包工具.exe (v$ver)" -ForegroundColor Green
} else {
    Write-Host '构建失败，请查看上方错误信息' -ForegroundColor Red
}
exit $LASTEXITCODE

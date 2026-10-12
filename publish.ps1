# publish.ps1 - 一键打包 + 自动提交 + 推送码云
# 用法：双击 一键发布.bat（或直接执行本脚本）
# 流程：1. 调用 build.ps1 完成混淆构建
#       2. 有代码改动时自动 git add -A + commit（版本号 + 时间作说明）
#       3. 自动推送到码云（凭据已缓存，不会弹窗）
# 说明：exe 成品在 build\bin\Python打包工具.exe；
#       发布到码云 Releases 需私人令牌，另行配置
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

# ---------- 1. 打包 ----------
Write-Host ''
Write-Host '========== 第 1 步：打包 ==========' -ForegroundColor Cyan
# 用子进程调用 build.ps1：它的 exit 只影响子进程，不会中断本脚本后续步骤
powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $root 'build.ps1')
if ($LASTEXITCODE -ne 0) {
    Write-Host '[错误] 打包失败，已停止（不会提交/推送）' -ForegroundColor Red
    exit 1
}

# ---------- 2. 自动提交 ----------
Write-Host ''
Write-Host '========== 第 2 步：自动提交 ==========' -ForegroundColor Cyan
Push-Location $root
$verMatch = Select-String -Path (Join-Path $root 'internal\version\version.go') -Pattern 'Version\s*=\s*"([^"]+)"'
$ver = if ($verMatch) { $verMatch.Matches[0].Groups[1].Value } else { '?' }
$time = Get-Date -Format 'yyyy-MM-dd HH:mm'
git add -A
$changed = git status --porcelain
if ($changed) {
    git commit -m "自动发布 v$ver ($time)"
    if ($LASTEXITCODE -ne 0) {
        Write-Host '[警告] git 提交失败' -ForegroundColor Yellow
    } else {
        Write-Host ("已自动提交: " + ($changed -split "`n").Count + ' 个文件')
    }
} else {
    Write-Host '无代码改动，跳过提交'
}
Pop-Location

# ---------- 3. 推送码云 ----------
Write-Host ''
Write-Host '========== 第 3 步：推送码云 ==========' -ForegroundColor Cyan
Push-Location $root
$env:GIT_HTTP_LOW_SPEED_LIMIT = '1'
$env:GIT_HTTP_LOW_SPEED_TIME = '30'
git push https://gitee.com/yizhishudaixiong/dabao.git master 2>&1
if ($LASTEXITCODE -eq 0) {
    Write-Host '码云已同步最新代码' -ForegroundColor Green
} else {
    Write-Host '[错误] 推送码云失败，请检查网络或凭据' -ForegroundColor Red
}
Pop-Location

Write-Host ''
Write-Host '========== 完成 ==========' -ForegroundColor Green
Write-Host 'exe 成品: build\bin\Python打包工具.exe'
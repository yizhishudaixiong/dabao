@echo off
rem ============================================
rem  One-click build + publish of Python Packager
rem  Double-click: build -> git commit -> push Gitee
rem  Change version in internal\version\version.go
rem ============================================
cd /d "%~dp0"
echo.
echo Building and publishing, please wait...
echo.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0publish.ps1"
echo.
echo Done. Press any key to close this window...
pause >nul

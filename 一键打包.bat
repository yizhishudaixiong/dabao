@echo off
rem ============================================
rem  One-click build of Python Packager tool
rem  Double-click this file to build.
rem  Build logic: build.ps1 (obfuscated build)
rem  Change version in internal\version\version.go
rem ============================================
cd /d "%~dp0"
echo.
echo Building, please wait...
echo.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1"
echo.
echo Build finished. Press any key to close this window...
pause >nul

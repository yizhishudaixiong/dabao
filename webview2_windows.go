//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/webviewloader"
)

// ensureWebView2 检测系统是否已安装 WebView2 Runtime，缺失时弹出原生提示框引导安装。
// 使用 Wails 底层的微软官方检测 API（GetAvailableCoreWebView2BrowserVersionString），
// 自动覆盖系统级/用户级安装、32/64 位视图、Edge 内置组件等所有安装场景。
// 返回 error 表示缺失且用户已被告知（此时应终止应用启动）。
func ensureWebView2() error {
	if hasWebView2Runtime() {
		return nil
	}
	message := "未检测到 Microsoft Edge WebView2 Runtime。\n\nPython打包工具 需要它来渲染界面，请先安装（约 1 分钟）。\n\n下载地址：\nhttps://developer.microsoft.com/microsoft-edge/webview2/\n\n安装完成后请重新打开本工具。"
	title := "Python打包工具 - 缺少运行组件"
	messageBox(message, title)
	return fmt.Errorf("WebView2 Runtime 未安装，已提示用户")
}

// hasWebView2Runtime 调用微软官方 API 检测 WebView2 Runtime 是否可用。
// 返回值非空即表示系统里有可用的 WebView2（无论装在哪个注册表位置）。
func hasWebView2Runtime() bool {
	ver, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err != nil {
		return false
	}
	return ver != ""
}

var (
	modUser32       = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = modUser32.NewProc("MessageBoxW")
)

const (
	mbOK       = 0x00000000
	mbIconInfo = 0x00000040
	hwndNone   = 0
)

func messageBox(text, title string) {
	procMessageBoxW.Call(
		uintptr(hwndNone),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		uintptr(mbOK|mbIconInfo),
	)
}

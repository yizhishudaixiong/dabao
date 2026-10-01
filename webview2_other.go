//go:build !windows

package main

// ensureWebView2 在非 Windows 平台不执行任何检查（仅用于本地开发编译）
func ensureWebView2() error {
	return nil
}

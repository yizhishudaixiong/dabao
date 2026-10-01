package main

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Windows 上先检查 WebView2 Runtime，缺失则原生弹窗提示用户安装（不嵌入，保持体积最小）
	if err := ensureWebView2(); err != nil {
		log.Printf("[Python打包工具] WebView2 检查失败: %v", err)
		return
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Python打包工具--🐨一只树袋熊出品",
		Width:     1080,
		Height:    720,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 244, G: 245, B: 248, A: 1},
		// 启用文件拖拽：把 .py 拖进窗口自动设为入口脚本、其他文件自动加入打包资源
		// 注意：Wails 默认关闭拖拽，不开启则前端 OnFileDrop 收不到任何事件
		// DisableWebViewDrop 同时禁止 WebView2 默认的"拖入打开文件"行为，避免冲突
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		OnStartup:        app.startup,
		// 窗口关闭拦截：任何任务进行中（打包/安装依赖/依赖分析/资源分析/便携版下载）时，
		// 阻止关闭并通知前端弹出自定义确认框（与软件风格一致）；
		// 用户在前端点「继续退出」后 ConfirmExit 置位，再次触发本回调时放行真正退出
		OnBeforeClose: func(ctx context.Context) bool {
			if app.IsBusy() {
				if app.IsExitConfirmed() {
					return false // 已确认退出 → 放行
				}
				// 阻止关闭，通知前端弹出确认框
				runtime.EventsEmit(ctx, "app:exit-request")
				return true
			}
			return false // 空闲时正常关闭
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatalf("Python打包工具 启动失败: %v", err)
	}
}

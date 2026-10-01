import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './styles/theme.css'
import * as AppBindings from '../wailsjs/go/main/App'

// 兼容 wails 混淆模式（-obfuscated / garble）：
// 混淆模式下 wails 不再按原名注册 window.go 方法，只注入 window.ObfuscatedCall(id, args)，
// 而本项目前端代码一直通过 window.go.main.App.xxx 调用。
// 这里检测到混淆模式时，把生成的绑定（ObfuscatedCall 封装）挂到 window.go.main.App 上，
// 让现有调用方式继续正常工作；普通构建（无混淆）时不做任何覆盖。
if (typeof (window as any).ObfuscatedCall === 'function') {
  const w = window as any
  w.go = w.go || {}
  w.go.main = w.go.main || {}
  w.go.main.App = AppBindings
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)

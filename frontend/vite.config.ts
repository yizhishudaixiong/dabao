import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Wails 在开发模式通过 vite server 注入 window.go / window.runtime
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsInlineLimit: 0,
  },
  server: {
    port: 5173,
    strictPort: true,
  },
})

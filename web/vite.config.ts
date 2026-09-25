import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const admin = process.env.CODEX_GATEWAY_ADMIN_URL ?? 'http://127.0.0.1:8081'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  build: {
    chunkSizeWarningLimit: 800,
    outDir: '../internal/admin/dist/app',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: admin,
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on('proxyReq', (req) => req.setHeader('origin', admin))
        },
      },
    },
  },
})

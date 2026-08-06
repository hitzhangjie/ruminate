import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Built assets are embedded by the Go server (internal/serve/static/dist).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/serve/static/dist',
    emptyOutDir: true,
  },
  server: {
    // Optional HMR-only workflow: npm run dev proxies API to ruminate serve.
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8420',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://127.0.0.1:8420',
        ws: true,
      },
    },
  },
})

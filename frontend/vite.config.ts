import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Split dev mode: Vite serves the dashboard with HMR on :5173 and proxies API
// calls to the Go process on :9300. For release, `bun run build` emits the
// bundle into the Go embed directory so the binary serves everything itself.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:9300',
    },
  },
})

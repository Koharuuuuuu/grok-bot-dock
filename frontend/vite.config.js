import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: { outDir: '../internal/webui/dist', emptyOutDir: true },
  server: { proxy: { '/api': 'http://localhost:18765', '/healthz': 'http://localhost:18765' } },
  test: { environment: 'jsdom' },
})

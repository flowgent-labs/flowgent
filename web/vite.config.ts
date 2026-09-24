import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '^/api/': {
        target: process.env.FLOWGENT_API_URL ?? 'http://127.0.0.1:9999',
        changeOrigin: true,
      },
      '^/_/': {
        target: process.env.FLOWGENT_API_URL ?? 'http://127.0.0.1:9999',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    include: ['src/**/*.test.{ts,tsx}'],
    exclude: ['node_modules/**'],
    coverage: { provider: 'v8', reporter: ['text', 'html'] },
  },
})

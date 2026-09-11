/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { DEFAULT_THEME, themeCss } from './src/theme/theme'

const apiTarget = process.env.FORMATIONS_API_URL

export default defineConfig({
  plugins: [react(), {
    name: 'canonical-theme-first-paint',
    transformIndexHtml: () => [{ tag: 'style', attrs: { id: 'default-theme' }, children: themeCss(DEFAULT_THEME), injectTo: 'head-prepend' }],
  }],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
    exclude: ['tests/**', 'node_modules/**', 'dist/**'],
    coverage: {
      provider: 'v8',
      reportsDirectory: './coverage',
      reporter: ['text', 'html', 'json-summary'],
      thresholds: {
        lines: 75,
        functions: 69,
        branches: 65,
        statements: 72,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    forwardConsole: false,
    proxy: apiTarget ? { '/api': { target: apiTarget, changeOrigin: true, ws: true } } : undefined,
  },
})

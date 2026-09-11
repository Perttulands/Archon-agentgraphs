import { defineConfig } from '@playwright/test'

const port = Number(process.env.FORMATIONS_BROWSER_PORT || 8193)
export default defineConfig({
  testDir: './tests',
  workers: 1,
  use: { baseURL: `http://127.0.0.1:${port}`, viewport: { width: 1440, height: 1000 }, screenshot: 'only-on-failure' },
  webServer: {
    command: `npm run dev -- --host 127.0.0.1 --port ${port} --strictPort`,
    url: `http://127.0.0.1:${port}`, reuseExistingServer: false,
  },
})

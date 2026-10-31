import { defineConfig, devices } from '@playwright/test'

// Drives the real page against a real hub (npm run e2e:real), not the mock. There is no web
// server to start: the hub is already running, and RN_BASE_URL says where (with its base path,
// ending in a slash, when it has one). The specs use relative URLs so the path is kept.
const base = process.env.RN_BASE_URL ?? 'http://127.0.0.1:8080/'

export default defineConfig({
  // not in e2e/, which the mock suite runs whole
  testDir: 'e2e-real',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 240_000,
  reporter: 'list',
  expect: { timeout: 20_000 },
  outputDir: 'test-results-real',
  use: {
    baseURL: base.endsWith('/') ? base : `${base}/`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
})

import { defineConfig } from '@playwright/test';

// The decision receipt suite: both engines, against the isolated harness.
const port = process.env.LANE_PORT ?? process.env.CODEAF_UI_PORT ?? '1712';
export default defineConfig({
  testDir: './src/features/decisions',
  testMatch: 'ReceiptLine.test.tsx',
  fullyParallel: true,
  workers: 2,
  outputDir: process.env.CODEAF_UI_RESULTS ?? '/tmp/codeaf-receipt-line-results',
  reporter: 'list',
  use: { baseURL: `http://127.0.0.1:${port}`, viewport: { width: 900, height: 700 }, trace: 'retain-on-failure' },
  webServer: {
    command: 'npx vite --config tests/receipt-line/vite.config.ts',
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer: false,
    env: { LANE_PORT: String(port), CHOKIDAR_USEPOLLING: '1' },
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

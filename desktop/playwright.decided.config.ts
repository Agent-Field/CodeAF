import { defineConfig } from '@playwright/test';

// Decided-rows suite. The port is this lane's so it does not take the app dev server.
const port = process.env.CODEAF_UI_PORT ?? '1894';
export default defineConfig({
  testDir: './src/features/decisions',
  testMatch: 'DecidedRows.test.tsx',
  fullyParallel: false,
  workers: 1,
  outputDir: process.env.CODEAF_UI_RESULTS ?? '/tmp/codeaf-decided-results',
  reporter: 'list',
  use: { baseURL: `http://127.0.0.1:${port}`, viewport: { width: 1200, height: 800 }, trace: 'retain-on-failure' },
  webServer: {
    command: `npx vite --config tests/decided/vite.config.ts --port ${port}`,
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer: false,
    env: { ...process.env, CODEAF_UI_PORT: port, CHOKIDAR_USEPOLLING: '1' },
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

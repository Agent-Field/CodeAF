import { defineConfig } from '@playwright/test';

// The overturn flow suite: both engines, against the isolated harness on port 1717.
export default defineConfig({
  testDir: './src/features/decisions',
  testMatch: 'OverturnFlow.test.tsx',
  fullyParallel: true, workers: 2,
  outputDir: '/tmp/codeaf-overturn-flow-results', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:1717', viewport: { width: 900, height: 600 }, trace: 'retain-on-failure' },
  webServer: { command: 'npx vite --config tests/overturn-flow/vite.config.ts', url: 'http://127.0.0.1:1717', reuseExistingServer: !process.env.CI, env: { CHOKIDAR_USEPOLLING: '1' } },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

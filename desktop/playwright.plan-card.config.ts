import { defineConfig } from '@playwright/test';

// The plan card suite: both engines, against the isolated harness on port 1707.
export default defineConfig({
  testDir: './src/features/decisions',
  testMatch: 'PlanCard.test.tsx',
  fullyParallel: true, workers: 2,
  outputDir: '/tmp/codeaf-plan-card-results', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:1707', viewport: { width: 900, height: 600 }, trace: 'retain-on-failure' },
  webServer: { command: 'npx vite --config tests/plan-card/vite.config.ts', url: 'http://127.0.0.1:1707', reuseExistingServer: !process.env.CI, env: { CHOKIDAR_USEPOLLING: '1' } },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

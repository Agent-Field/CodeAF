import { defineConfig } from '@playwright/test';

// The Using list suite: both engines, against the isolated harness on port 1757.
export default defineConfig({
  testDir: './tests/using',
  fullyParallel: true, workers: 2,
  outputDir: '/tmp/codeaf-using-results', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:1757', viewport: { width: 1200, height: 900 }, trace: 'retain-on-failure' },
  webServer: { command: 'npx vite --config tests/using/vite.config.ts', url: 'http://127.0.0.1:1757', reuseExistingServer: !process.env.CI, env: { CHOKIDAR_USEPOLLING: '1' } },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

import { defineConfig } from '@playwright/test';

// The Places Home suite: both engines, against the isolated harness on port 1745.
export default defineConfig({
  testDir: './tests/places-home',
  fullyParallel: true, workers: 2,
  outputDir: '/tmp/codeaf-places-home-results', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:1745', viewport: { width: 1200, height: 900 }, trace: 'retain-on-failure' },
  webServer: { command: 'npx vite --config tests/places-home/vite.config.ts', url: 'http://127.0.0.1:1745', reuseExistingServer: !process.env.CI, env: { CHOKIDAR_USEPOLLING: '1' } },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

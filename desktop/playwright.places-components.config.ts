import { defineConfig } from '@playwright/test';

// The Places primitives suite: both engines, against the isolated harness on port 1711.
export default defineConfig({
  testDir: './tests/places-components',
  fullyParallel: true, workers: 2,
  outputDir: '/tmp/codeaf-places-components-results', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:1711', viewport: { width: 1200, height: 900 }, trace: 'retain-on-failure' },
  webServer: { command: 'npx vite --config tests/places-components/vite.config.ts', url: 'http://127.0.0.1:1711', reuseExistingServer: !process.env.CI },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

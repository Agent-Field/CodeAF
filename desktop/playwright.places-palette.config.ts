import { defineConfig } from '@playwright/test';

// The ⌘P palette suite: both engines, against its own harness on port 1738.
export default defineConfig({
  testDir: './tests/places-palette',
  fullyParallel: true, workers: 2,
  outputDir: process.env.PW_OUT ?? '/tmp/codeaf-places-palette-results', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:1738', viewport: { width: 1200, height: 900 }, trace: 'retain-on-failure' },
  webServer: { command: 'npx vite --config tests/places-palette/vite.config.ts', url: 'http://127.0.0.1:1738', reuseExistingServer: false },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

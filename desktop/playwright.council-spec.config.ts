import { defineConfig } from '@playwright/test';

// Council chat spec: the council pane harness (this lane's port) and the Places Home harness (1745) that draws the Live row.
const port = process.env.CODEAF_UI_PORT ?? '1776';
const env = { ...process.env, CODEAF_UI_PORT: port, CHOKIDAR_USEPOLLING: '1' };
export default defineConfig({
  testDir: './tests/ui',
  testMatch: 'council.spec.ts',
  fullyParallel: false,
  workers: 1,
  outputDir: process.env.CODEAF_UI_RESULTS ?? '/tmp/codeaf-council-spec-results',
  reporter: 'list',
  use: { viewport: { width: 1000, height: 800 }, trace: 'retain-on-failure' },
  webServer: [
    { command: 'npx vite --config tests/council-wire/vite.config.ts', url: `http://127.0.0.1:${port}`, reuseExistingServer: false, env },
    { command: 'npx vite --config tests/places-home/vite.config.ts', url: 'http://127.0.0.1:1745', reuseExistingServer: false, env },
  ],
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

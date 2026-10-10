import { defineConfig } from '@playwright/test';

// The why popover suite: both engines, against the isolated harness on port 1713.
export default defineConfig({
  testDir: './src/features/decisions',
  testMatch: 'WhyPopover.test.tsx',
  fullyParallel: true, workers: 2,
  outputDir: '/tmp/codeaf-why-popover-results', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:1713', viewport: { width: 900, height: 600 }, trace: 'retain-on-failure' },
  webServer: { command: 'npx vite --config tests/why-popover/vite.config.ts', url: 'http://127.0.0.1:1713', reuseExistingServer: !process.env.CI, env: { CHOKIDAR_USEPOLLING: '1' } },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

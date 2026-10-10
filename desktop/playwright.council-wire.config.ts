import { defineConfig } from '@playwright/test';

// Council pane suite, against the fake-stream harness. The port is this lane's so it does not take the app dev server.
const port = process.env.CODEAF_UI_PORT ?? '1758';
export default defineConfig({
  testDir: './src/features/council',
  testMatch: 'CouncilPane.test.tsx',
  fullyParallel: false,
  workers: 1,
  outputDir: process.env.CODEAF_UI_RESULTS ?? '/tmp/codeaf-council-wire-results',
  reporter: 'list',
  use: { baseURL: `http://127.0.0.1:${port}`, viewport: { width: 1000, height: 800 }, trace: 'retain-on-failure' },
  webServer: { command: 'npx vite --config tests/council-wire/vite.config.ts', url: `http://127.0.0.1:${port}`, reuseExistingServer: false, env: { CHOKIDAR_USEPOLLING: '1', CODEAF_UI_PORT: port } },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

import { defineConfig } from '@playwright/test';

// The workspace-sync suite: two real pages on one place against the REAL bridge (bin/codeaf, `make build` first),
// one worker so its long polls never compete with another suite's, both engines.
export default defineConfig({
  testDir: './tests/workspace-sync',
  fullyParallel: false, workers: 1,
  outputDir: '/tmp/codeaf-workspace-sync-results', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:1771', viewport: { width: 1000, height: 700 }, trace: 'retain-on-failure' },
  webServer: [
    { command: 'node tests/workspace-sync/bridge.mjs', url: 'http://127.0.0.1:17712/api/engine/health', reuseExistingServer: false },
    { command: 'npx vite --config tests/workspace-sync/vite.config.ts', url: 'http://127.0.0.1:1771', reuseExistingServer: false },
  ],
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

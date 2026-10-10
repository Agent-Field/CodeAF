import { defineConfig } from '@playwright/test';
// This isolated harness tests the primitive without the shell; the owning pages wire shortcuts separately.
const port = process.env.CODEAF_UI_PORT ?? '1774';
export default defineConfig({
 testDir: '.', testMatch: ['**/tests/search-field/*.spec.ts'], fullyParallel: true, workers: 2,
 outputDir: process.env.CODEAF_UI_RESULTS ?? '/tmp/codeaf-search-field-results', reporter: 'list',
 use: { baseURL: `http://127.0.0.1:${port}`, viewport: { width: 1200, height: 800 }, trace: 'retain-on-failure' },
 webServer: { command: `npx vite --configLoader runner --config tests/search-field/vite.config.ts --port ${port}`, url: `http://127.0.0.1:${port}`, reuseExistingServer: false },
 projects: [{name:'chromium',use:{browserName:'chromium'}},{name:'webkit',use:{browserName:'webkit'}}],
});

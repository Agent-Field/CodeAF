import { defineConfig } from '@playwright/test';
export default defineConfig({
 testDir: './tests/ui', fullyParallel: true, workers: 2,
 outputDir: '/tmp/codeaf-app-ui-results', reporter: 'list',
 use: { baseURL: 'http://127.0.0.1:1422', viewport: { width: 1200, height: 800 }, trace: 'retain-on-failure' },
 webServer: { command: 'npm run dev -- --port 1422', url: 'http://127.0.0.1:1422', reuseExistingServer: !process.env.CI },
 projects: [{name:'chromium',use:{browserName:'chromium'}},{name:'webkit',use:{browserName:'webkit'}}],
});

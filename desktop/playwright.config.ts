import { defineConfig } from '@playwright/test';
// CODEAF_UI_PORT lets several worktrees run the suite side by side; reusing another checkout's dev server would test its code, not this one's.
const port = process.env.CODEAF_UI_PORT ?? '1422';
export default defineConfig({
 testDir: '.', testMatch: ['**/tests/ui/**/*.spec.ts', '**/src/features/nextup/Banner.test.tsx', '**/src/features/nextup/FramePill.test.tsx'], fullyParallel: true, workers: 2,
 outputDir: process.env.CODEAF_UI_RESULTS ?? '/tmp/codeaf-app-ui-results', reporter: 'list',
 use: { baseURL: `http://127.0.0.1:${port}`, viewport: { width: 1200, height: 800 }, trace: 'retain-on-failure' },
 webServer: { command: `npm run dev -- --port ${port}`, url: `http://127.0.0.1:${port}`, reuseExistingServer: !process.env.CI },
 projects: [{name:'chromium',use:{browserName:'chromium'}},{name:'webkit',use:{browserName:'webkit'}}],
});

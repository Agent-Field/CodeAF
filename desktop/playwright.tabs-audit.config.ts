import { defineConfig } from '@playwright/test';

// The complete tabs audit (docs/TABS-COMPLETE-AUDIT.md). Its journeys assert the design, so on a tree that has not
// built a feature yet they fail and name the gap; that is why they live outside tests/ui and the default gate. Run it
// on its own port with a polling watcher and one worker, so it never competes with another lane's dev server.
const port = Number(process.env.TABS_AUDIT_PORT ?? 1759);
export default defineConfig({
 testDir: './tests/tabs-audit', fullyParallel: true, workers: Number(process.env.TABS_AUDIT_WORKERS ?? 1),
 outputDir: '/tmp/codeaf-tabs-audit-results', reporter: 'list', timeout: 60_000,
 use: { baseURL: `http://127.0.0.1:${port}`, viewport: { width: 1200, height: 800 }, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
 webServer: { command: `npm run dev -- --port ${port} --strictPort`, env: { CHOKIDAR_USEPOLLING: '1' }, url: `http://127.0.0.1:${port}`, reuseExistingServer: true, timeout: 180_000 },
 projects: [{ name: 'chromium', use: { browserName: 'chromium' } }, { name: 'webkit', use: { browserName: 'webkit' } }],
});

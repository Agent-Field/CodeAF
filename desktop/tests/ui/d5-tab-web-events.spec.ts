import { test, expect } from '@playwright/test';
import { emitState, installNativeWebMock, nativeCalls, seedWebTab, MOCK_SHOT } from './support/native-web-mock';
import { installMockEngine } from './support/mock-engine';
import { richReply } from './support/scenarios-v2';
import { savedWorkspace } from './support/synced-workspace';
import type { mountWebEventsProbe } from './support/web-events-probe';

declare global { interface Window { webEventsProbe: ReturnType<typeof mountWebEventsProbe> } }
const pane = 'webpane1';
const next = 'https://pkg.go.dev/encoding/json#Encoder';

for (const theme of ['light', 'dark']) {
  test(`${theme}: page identity, loading, history, favicon and relaunch follow the real native state`, async ({ page }) => {
    await installNativeWebMock(page, { engine: true });
    await installMockEngine(page, richReply());
    let iconReady = false;
    await page.route('**/api/engine/workspaces/*/favicon?*', route => route.fulfill({ json: { domain: 'pkg.go.dev', dataUrl: iconReady ? MOCK_SHOT : undefined } }));
    await seedWebTab(page);
    await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme);
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    const tab = page.locator('.workspace-tab[data-kind="web"]');
    await expect(tab.locator('span.tab-monogram')).toHaveText('p');
    const loading = page.getByRole('progressbar', { name: 'Loading page' });
    await expect(loading).toBeVisible();
    expect(await loading.evaluate(el => getComputedStyle(el).height)).toBe('2px');
    const line = (await loading.boundingBox())!;
    const card = (await page.locator('.workspace-pane').boundingBox())!;
    expect(line.y).toBe(card.y);
    await expect(tab.locator('[role="progressbar"], .tab-state-dot')).toHaveCount(0);

    await emitState(page, 'another-pane', { title: 'Wrong page', url: 'https://example.org', loading: false });
    await expect(tab).not.toContainText('Wrong page');
    iconReady = true;
    await emitState(page, pane, { title: 'JSON reference', url: next, loading: false, canBack: true, canForward: false });
    await expect(tab).toContainText('JSON reference');
    await expect(loading).toHaveCount(0);
    await expect(tab.locator('img.tab-monogram')).toHaveAttribute('src', MOCK_SHOT);
    await expect(page.getByRole('button', { name: `Address ${next}. Edit address`, exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Back', exact: true })).toBeEnabled();
    await expect(page.getByRole('button', { name: 'Forward', exact: true })).toBeDisabled();
    await page.getByRole('button', { name: 'Back', exact: true }).click();
    expect((await nativeCalls(page, 'web_history')).at(-1)?.args).toEqual({ pane, step: 'back' });
    await emitState(page, pane, { canBack: false, canForward: true });
    await expect(page.getByRole('button', { name: 'Forward', exact: true })).toBeEnabled();
    await page.getByRole('button', { name: 'Forward', exact: true }).click();
    expect((await nativeCalls(page, 'web_history')).at(-1)?.args).toEqual({ pane, step: 'forward' });
    await expect.poll(async () => (await savedWorkspace(page)).tabs.find(tab => tab.id === pane)?.target?.url).toBe(next);
    await page.reload();
    await expect.poll(async () => (await nativeCalls(page, 'web_open'))[0]?.args.url).toBe(next);
    await expect(page.getByRole('button', { name: `Address ${next}. Edit address`, exact: true })).toBeVisible();
  });

  test(`${theme}: a manual tab name wins over subsequent page titles and survives reload`, async ({ page }) => {
    await installNativeWebMock(page, { engine: true });
    await installMockEngine(page, richReply());
    await seedWebTab(page);
    await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme);
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, pane, { title: 'JSON reference', loading: false });
    const tab = page.locator('.workspace-tab[data-kind="web"]');
    await tab.getByRole('tab').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Rename tab', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Rename tab', exact: true });
    await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill('My reference');
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await emitState(page, pane, { title: 'A new page title', url: next });
    await expect(tab).toContainText('My reference');
    await expect.poll(async () => (await savedWorkspace(page)).tabs.find(tab => tab.id === pane)?.titleSource).toBe('manual');
    await page.reload();
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, pane, { title: 'Another title', loading: false });
    await expect(tab).toContainText('My reference');
  });
}

test('same-title navigation refreshes the summary without republishing on callback-only renders', async ({ page }) => {
  await page.route('**/api/engine/**', route => route.abort());
  await page.goto('/');
  await page.evaluate(async () => {
    const path = '/tests/ui/support/web-events-probe.tsx';
    window.webEventsProbe = (await import(path)).mountWebEventsProbe();
    window.webEventsProbe.render({ pane: 'probe', title: 'JSON reference', url: 'https://pkg.go.dev/encoding/json#Decoder', loading: false, canBack: false, canForward: false, historyKnown: true, failure: null, notice: null });
  });
  await expect.poll(() => page.evaluate(() => window.webEventsProbe.summaries.length)).toBe(1);
  await page.evaluate(url => window.webEventsProbe.render({ pane: 'probe', title: 'JSON reference', url, loading: false, canBack: true, canForward: false, historyKnown: true, failure: null, notice: null }), next);
  await expect.poll(() => page.evaluate(() => window.webEventsProbe.summaries.at(-1)?.digest)).toBe(next);
  await page.evaluate(url => window.webEventsProbe.render({ pane: 'probe', title: 'JSON reference', url, loading: false, canBack: true, canForward: false, historyKnown: true, failure: null, notice: null }), next);
  // Wait for React to commit a callback-only render, without a real-time sleep.
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  expect(await page.evaluate(() => window.webEventsProbe.summaries.length)).toBe(2);
  await page.evaluate(() => window.webEventsProbe.dispose());
});

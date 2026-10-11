import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';
import { openPage } from './support/shell-navigation';

// CSS mode is selected explicitly: browser coverage proves paint, not native macOS vibrancy.
async function native(page: Page) {
 await page.evaluate(() => {
  document.documentElement.dataset.environment = 'mac-desktop';
  document.documentElement.dataset.material = 'native';
  // Engine renders trigger the browser material observer; keep this CSS-only native fixture selected.
  const root = document.documentElement;
  new MutationObserver(() => { if (root.dataset.material !== 'native') root.dataset.material = 'native'; }).observe(root, { attributes: true, attributeFilter: ['data-material'] });
 });
}
async function alpha(locator: Locator) {
 return locator.evaluate(el => {
  // Canvas normalizes both engines' computed oklch colors to an alpha byte.
  const canvas = document.createElement('canvas'); canvas.width = canvas.height = 1;
  const context = canvas.getContext('2d')!;
  context.fillStyle = getComputedStyle(el).backgroundColor;
  context.fillRect(0, 0, 1, 1);
  return context.getImageData(0, 0, 1, 1).data[3];
 });
}
for (const theme of ['light', 'dark'] as const) {
 test(`${theme}: native frame is translucent and content owns opaque paper`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, pendingQuestion());
  await page.addInitScript(() => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'chat', kind: 'conversation', title: 'Storage', draft: '', pinned: false, sessionFile: 'chat.jsonl' }], groups: [], closed: [], activeId: 'chat', nextNumber: 2, recentIds: ['chat'] })));
  await page.goto('/');
  await expect(page.locator('.workspace-pane')).toBeVisible();
  await native(page);
  await expect.poll(() => alpha(page.locator('.app-shell'))).toBeGreaterThan(0);
  await expect.poll(() => alpha(page.locator('.app-shell'))).toBeLessThan(255);
  for (const selector of ['body', '.sidebar', '.workspace-tabbar']) {
   await expect.poll(() => alpha(page.locator(selector))).toBe(0);
   await expect(page.locator(selector)).toHaveCSS('backdrop-filter', 'none');
  }
  expect(await alpha(page.locator('.workspace-pane'))).toBe(255);
  for (const selector of ['.conversation-main', '.composer', '.decision-tray']) {
   await expect(page.locator(selector)).toBeVisible();
   expect(await alpha(page.locator(selector))).toBe(255);
  }
  // Standalone paint probes assert overlay paper independently of any opaque ancestor.
  const classes = ['content-card', 'hover-preview', 'app-menu', 'select-menu', 'quick-look', 'workspace-rename'];
  await page.evaluate(classes => {
   const container = document.createElement('div'); container.id = 'material-probes';
   for (const name of classes) { const surface = document.createElement('div'); surface.className = name; container.append(surface); }
   document.body.append(container);
  }, classes);
  await native(page);
  for (const name of classes) expect(await alpha(page.locator(`#material-probes .${name}`))).toBe(255);
  await page.locator('#material-probes').evaluate(el => el.remove());
  await openPage(page, 'Settings');
  await native(page);
  for (const selector of ['.settings-scroll', '.settings-page']) expect(await alpha(page.locator(selector))).toBe(255);
 });
 test(`${theme}: native peeking chrome has no second blur and accessibility paints solid`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, pendingQuestion());
  await page.addInitScript(() => localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({ tabs: [{ id: 'chat', kind: 'conversation', title: 'Storage', draft: '', pinned: false, sessionFile: 'chat.jsonl' }], groups: [], closed: [], activeId: 'chat', nextNumber: 2, recentIds: ['chat'] })));
  await page.goto('/');
  await expect(page.locator('.workspace-pane')).toBeVisible();
  await page.evaluate(() => document.querySelector('.app-shell')!.classList.add('sidebar-collapsed'));
  await native(page);
  await expect(page.locator('.sidebar')).toHaveCSS('backdrop-filter', 'none');
  await page.evaluate(() => document.querySelector('.app-shell')!.setAttribute('data-focus', ''));
  await expect(page.locator('.workspace-tabbar')).toHaveCSS('backdrop-filter', 'none');
  await page.emulateMedia({ contrast: 'more' });
  await expect.poll(() => alpha(page.locator('.app-shell'))).toBe(255);
  await expect.poll(() => alpha(page.locator('.sidebar'))).toBe(255);
 });
}

for (const supported of [false, true]) {
 test(`Windows native material support ${supported} selects ${supported ? 'native' : 'solid'}`, async ({ page }) => {
  await page.addInitScript(supported => {
   Object.defineProperty(navigator, 'platform', { get: () => 'Win32' });
   Object.assign(window, {
    isTauri: true,
    __TAURI_INTERNALS__: {
     invoke: async (cmd: string, args: Record<string, unknown> = {}) => {
      if (cmd === 'plugin:event|listen') return args.handler;
      if (cmd === 'window_context') return { label: 'main', placeKey: 'now', nativeMaterial: supported };
      if (cmd === 'engine_connection') throw new Error('No native engine in this fixture');
      return null;
     },
     transformCallback: () => 1,
     unregisterCallback: () => {},
     metadata: { currentWindow: { label: 'main' }, currentWebview: { windowLabel: 'main', label: 'main' } },
    },
    __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener: () => {} },
   });
  }, supported);
  await page.route('**/api/engine/**', route => route.abort());
  await page.goto('/');
  await expect(page.locator('.app-shell')).toBeVisible();
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.locator('html')).toHaveAttribute('data-material', supported ? 'native' : 'solid');
  if (!supported) await expect.poll(() => alpha(page.locator('.app-shell'))).toBe(255);
 });
}

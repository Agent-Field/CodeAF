import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls } from './contracts';
import { emitNewTab, emitState, installNativeWebMock, MOCK_SHOT, nativeCalls, seedWebTab } from './support/native-web-mock';
import design from '../../src/design/tokens.json' with { type: 'json' };

// The web tab (design 3d). The browser has no native views, so these tests use
// a typed mock of web.rs at the IPC boundary: they prove what the renderer asks
// of the native side and what it draws from the answers, never that a page
// rendered. Native isolation is proven by the Rust tests in src-tauri/src/web/.
const PANE = 'webpane1';
const URL = 'https://pkg.go.dev/encoding/json#Decoder';
const px = (name: string) => parseFloat((design.foundation as Record<string, string>)[name]);

test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

async function sheetRect(page: Page) {
  const box = (await page.locator('.web-sheet').boundingBox())!;
  return { x: Math.round(box.x), y: Math.round(box.y), width: Math.round(box.width), height: Math.round(box.height) };
}
const lastVisible = async (page: Page) => (await nativeCalls(page, 'web_visible')).at(-1)?.args.visible;

test.describe('outside the desktop app', () => {
  test('a web tab says pages open in the desktop app and never pretends one loaded', async ({ page }) => {
    await seedWebTab(page);
    await page.goto('/');
    const sheet = page.locator('.web-sheet');
    await expect(sheet.getByText('Web pages open in the desktop app')).toBeVisible();
    await expect(page.locator('iframe, webview, object, embed')).toHaveCount(0);
    await expect(page.getByRole('progressbar', { name: 'Loading page' })).toHaveCount(0);
    for (const name of ['Back', 'Forward', 'Reload']) await expect(page.getByRole('button', { name, exact: true })).toBeDisabled();
    await expect(page.getByRole('button', { name: /^Address https:\/\/pkg\.go\.dev/ })).toBeVisible();
    const popup = page.waitForEvent('popup');
    await sheet.getByRole('button', { name: 'Open in browser' }).click();
    expect((await popup).url()).toBe(URL);
  });
});

test.describe('in the desktop app (typed native mock)', () => {
  test.beforeEach(async ({ page }) => { await installNativeWebMock(page); await seedWebTab(page); });

  test('opens one child view over the sheet with the pane id, the address and the measured rectangle', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    const open = (await nativeCalls(page, 'web_open'))[0].args;
    expect(open.pane).toBe(PANE);
    expect(open.url).toBe(URL);
    expect(open.rect).toEqual(await sheetRect(page));
    expect(Object.keys(open).sort()).toEqual(['pane', 'rect', 'url', 'visible']);
    // Design 3d geometry: 48px header, 28px controls, a 30px field at most 520px wide, an 8px sheet inset.
    expect((await page.locator('.web-header').boundingBox())!.height).toBe(px('web-header-height'));
    expect((await page.getByRole('button', { name: 'Back', exact: true }).boundingBox())!.width).toBe(28);
    const field = (await page.locator('.web-address').boundingBox())!;
    expect(field.height).toBe(px('web-address-height'));
    expect(field.width).toBeLessThanOrEqual(px('web-address-max-width'));
    await expect(page.locator('.web-sheet')).toHaveCSS('border-top-left-radius', '8px');
    await expect(page.locator('.web-address-site')).toHaveText('pkg.go.dev');
    await expect(page.locator('.web-address-rest')).toHaveText('/encoding/json#Decoder');
    // No renderer command ever carries a script, a path or a shell command.
    for (const call of await nativeCalls(page)) expect(JSON.stringify(call.args)).not.toMatch(/javascript:|file:|<script|\beval\b/i);
  });

  test('page state drives the load line, the title, history and stop', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await expect(page.getByRole('progressbar', { name: 'Loading page' })).toBeVisible();
    await page.getByRole('button', { name: 'Stop loading' }).click();
    expect((await nativeCalls(page, 'web_history')).at(-1)!.args).toEqual({ pane: PANE, step: 'stop' });
    await emitState(page, PANE, { loading: false, title: 'json package - encoding/json', canBack: true, canForward: false });
    await expect(page.getByRole('progressbar', { name: 'Loading page' })).toHaveCount(0);
    await expect(page.locator('.workspace-tab[data-active="true"]')).toContainText('json package - encoding/json');
    await expect(page.getByRole('button', { name: 'Forward', exact: true })).toBeDisabled();
    const back = page.getByRole('button', { name: 'Back', exact: true });
    await expect(back).toBeEnabled();
    await back.click();
    await page.getByRole('button', { name: 'Reload', exact: true }).click();
    const steps = (await nativeCalls(page, 'web_history')).map(call => call.args.step);
    expect(steps).toEqual(['stop', 'back', 'reload']);
  });

  test('history the platform cannot report leaves Back and Forward available', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: false, historyKnown: false, canBack: false, canForward: false });
    await expect(page.getByRole('button', { name: 'Back', exact: true })).toBeEnabled();
    await expect(page.getByRole('button', { name: 'Forward', exact: true })).toBeEnabled();
  });

  test('the address field goes by keyboard and refuses what a web tab may not open', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: false });
    await page.getByRole('button', { name: /^Address / }).focus();
    await page.keyboard.press('Enter');
    const input = page.getByRole('textbox', { name: 'Address' });
    await expect(input).toBeFocused();
    await expect(input).toHaveValue(URL);
    for (const bad of ['javascript:alert(1)', 'file:///etc/passwd', 'data:text/html,hi', 'tauri://localhost', 'https://user:pw@example.com']) {
      await input.fill(bad);
      await page.keyboard.press('Enter');
      await expect(page.getByRole('alert')).toBeVisible();
      await expect(input).toHaveAttribute('aria-invalid', 'true');
    }
    expect(await nativeCalls(page, 'web_navigate')).toEqual([]);
    await input.fill('go.dev/doc');
    await page.keyboard.press('Enter');
    await expect.poll(async () => (await nativeCalls(page, 'web_navigate')).map(c => c.args)).toEqual([{ pane: PANE, url: 'https://go.dev/doc' }]);
    await expect(page.locator('.web-address-site')).toHaveText('go.dev');
    // Escape puts the address back without going anywhere.
    await page.getByRole('button', { name: /^Address / }).click();
    await page.getByRole('textbox', { name: 'Address' }).fill('example.org');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', { name: /^Address https:\/\/go\.dev\/doc/ })).toBeFocused();
    expect((await nativeCalls(page, 'web_navigate')).length).toBe(1);
  });

  test('anything of the app over the page hides the view and shows the last picture; closing it restores the view', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: true });
    await emitState(page, PANE, { loading: false, title: 'encoding/json' });
    await expect.poll(async () => (await nativeCalls(page, 'web_snapshot')).length).toBeGreaterThan(0);
    // The overview is a modal: every web view hides under it.
    await page.getByRole('button', { name: /All tabs/ }).first().click();
    await expect(page.getByRole('dialog', { name: /All tabs overview/ })).toBeVisible();
    await expect.poll(() => lastVisible(page)).toBe(false);
    await expect(page.locator('img.web-frozen')).toHaveAttribute('src', MOCK_SHOT);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog', { name: /All tabs overview/ })).toHaveCount(0);
    await expect.poll(() => lastVisible(page)).toBe(true);
    await expect(page.locator('img.web-frozen')).toHaveCount(0);
    // A tab's context menu covers part of the card.
    await page.locator('.workspace-tab[data-active="true"]').click({ button: 'right' });
    await expect(page.getByRole('menu')).toBeVisible();
    await expect.poll(() => lastVisible(page)).toBe(false);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('menu')).toHaveCount(0);
    // Whatever stays up afterwards (the tab's preview card) keeps it hidden; it is never drawn under the page.
    if (await page.locator('.hover-preview').count()) expect(await lastVisible(page)).toBe(false);
  });

  test('a surface another lane marks as a cover hides the view only while it overlaps the sheet', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await expect.poll(() => lastVisible(page)).not.toBe(false);
    const sheet = await sheetRect(page);
    const place = (x: number, y: number) => page.evaluate(([left, top]) => {
      let toast = document.getElementById('probe-cover');
      if (!toast) { toast = document.createElement('div'); toast.id = 'probe-cover'; toast.dataset.nativeCover = ''; toast.className = 'probe-cover'; document.body.append(toast); }
      toast.setAttribute('style', `position:fixed;left:${left}px;top:${top}px;width:40px;height:20px`);
    }, [x, y] as const);
    await place(sheet.x + 10, sheet.y + 10);
    await expect.poll(() => lastVisible(page)).toBe(false);
    await place(0, 0);
    await expect.poll(() => lastVisible(page)).toBe(true);
    await place(sheet.x + sheet.width - 30, sheet.y + sheet.height - 15);
    await expect.poll(() => lastVisible(page)).toBe(false);
    await page.evaluate(() => document.getElementById('probe-cover')?.remove());
    await expect.poll(() => lastVisible(page)).toBe(true);
    expect((await nativeCalls(page, 'web_open')).length).toBe(1);
    expect(await nativeCalls(page, 'web_close')).toEqual([]);
  });

  test('a resize moves the view to the sheet once per frame; a tab switch hides it and keeps the page', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await page.setViewportSize({ width: 1000, height: 700 });
    await expect.poll(async () => (await nativeCalls(page, 'web_bounds')).at(-1)?.args.rect).toEqual(await sheetRect(page));
    await page.locator('.workspace-tab', { hasText: 'Config stack' }).click();
    await expect.poll(() => lastVisible(page)).toBe(false);
    await page.locator('.workspace-tab', { hasText: 'pkg.go.dev' }).click();
    await expect.poll(() => lastVisible(page)).toBe(true);
    expect((await nativeCalls(page, 'web_open')).length).toBe(1);
    expect(await nativeCalls(page, 'web_close')).toEqual([]);
    expect(await nativeCalls(page, 'web_navigate')).toEqual([]);
  });

  test('closing a web tab closes only its view; no engine call is made', async ({ page }) => {
    const engine: string[] = [];
    await page.route('**/api/engine/**', route => { engine.push(route.request().url()); return route.abort(); });
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await page.locator('.workspace-tab[data-active="true"]').hover();
    await page.locator('.workspace-tab[data-active="true"]').getByRole('button', { name: /^Close / }).click();
    await expect.poll(() => lastVisible(page)).toBe(false);
    // The workspace's reaper (useWorkspaceWeb) closes views whose panes are gone.
    await page.evaluate(async () => { const views = await import('/src/features/web/views.ts'); views.reap([]); });
    await expect.poll(async () => (await nativeCalls(page, 'web_close')).map(c => c.args)).toEqual([{ pane: PANE }]);
    expect(engine).toEqual([]);
  });

  test('a failed load shows why, hides the view, and Try again reloads', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: false, failure: { kind: 'unreachable' } });
    const alert = page.locator('.web-state[role="alert"]');
    await expect(alert).toContainText('This page did not load');
    await expect(alert).toContainText('codeaf could not reach pkg.go.dev.');
    await expect.poll(() => lastVisible(page)).toBe(false);
    await alert.getByRole('button', { name: 'Try again' }).click();
    expect((await nativeCalls(page, 'web_history')).at(-1)!.args.step).toBe('reload');
    await expect.poll(() => lastVisible(page)).toBe(true);
    await emitState(page, PANE, { loading: false, failure: { kind: 'refused', reason: 'appOrigin' } });
    await expect(alert).toContainText("codeaf's own pages cannot open in a web tab.");
  });

  test('a refused download or popup is one transient line in the address field', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: false, notice: 'download' });
    await expect(page.getByRole('status')).toHaveText('Downloads do not open in web tabs');
    await emitState(page, PANE, { notice: null });
    await expect(page.locator('.web-address-rest')).toHaveText('/encoding/json#Decoder');
  });

  test("a page's new window becomes a typed new-tab request, never a window", async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    // With no workspace host the link goes to the default browser, as a link chip does.
    await emitNewTab(page, PANE, 'https://go.dev/blog');
    await expect.poll(async () => (await nativeCalls(page, 'open_url')).map(c => c.args)).toEqual([{ url: 'https://go.dev/blog' }]);
    // With the host the workspace registers, it is a web tab request carrying its opener.
    await page.evaluate(async () => {
      const host = await import('/src/features/web/host.ts');
      const seen: unknown[] = [];
      (window as unknown as { seen: unknown[] }).seen = seen;
      host.setWebHost({ openWebTab: (url, opener) => seen.push({ url, opener }) });
    });
    await emitNewTab(page, PANE, 'https://go.dev/play');
    expect(await page.evaluate(() => (window as unknown as { seen: unknown[] }).seen)).toEqual([{ url: 'https://go.dev/play', opener: PANE }]);
  });

  test('starting a conversation with the page hands over its address and a fresh picture, and calls no engine', async ({ page }) => {
    const engine: string[] = [];
    await page.route('**/api/engine/**', route => { engine.push(route.request().url()); return route.abort(); });
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: false, title: 'encoding/json' });
    // Absent until a workspace can start one: never a button that does nothing.
    await expect(page.getByRole('button', { name: 'Start a conversation with this page' })).toHaveCount(0);
    await page.evaluate(async () => {
      const host = await import('/src/features/web/host.ts');
      const context = await import('/src/features/web/pageContext.ts');
      host.setWebHost({ openWebTab() {}, startConversationWithPage: p => { (window as unknown as { handed: unknown }).handed = { ...p, ...context.pageAttachment(p), files: context.pageAttachment(p).files.map(f => [f.name, f.type, f.size]) }; } });
    });
    const before = (await nativeCalls(page, 'web_snapshot')).length;
    await page.getByRole('button', { name: 'Start a conversation with this page' }).click();
    await expect.poll(() => page.evaluate(() => (window as unknown as { handed?: unknown }).handed)).toMatchObject({
      url: URL, title: 'encoding/json', shot: MOCK_SHOT, draft: `About this page, "encoding/json": ${URL}\n\n`, files: [['page.png', 'image/png', 70]],
    });
    expect((await nativeCalls(page, 'web_snapshot')).length).toBe(before + 1);
    expect(engine).toEqual([]);
  });

  for (const theme of ['light', 'dark'] as const) {
    for (const width of [320, 800, 1200]) {
      test(`${theme} at ${width}px: header controls stay reachable without overflow and pass accessibility`, async ({ page }) => {
        await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme);
        await page.setViewportSize({ width, height: width < 800 ? 560 : 800 });
        await page.goto('/');
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
        await emitState(page, PANE, { loading: false, title: 'encoding/json', canBack: true });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        const header = (await page.locator('.web-header').boundingBox())!;
        for (const name of ['Back', 'Forward', 'Reload', 'Open in browser']) {
          const box = (await page.getByRole('button', { name, exact: true }).boundingBox())!;
          expect(box.x).toBeGreaterThanOrEqual(header.x);
          expect(box.x + box.width).toBeLessThanOrEqual(header.x + header.width + 0.5);
        }
        await expect(page.locator('.web-address')).toBeVisible();
        // The view sits inside the sheet, which sits inside the window.
        const rect = (await nativeCalls(page, 'web_bounds')).at(-1)?.args.rect ?? (await nativeCalls(page, 'web_open'))[0].args.rect;
        expect(rect).toEqual(await sheetRect(page));
        await expectNoUnstyledControls(page);
        await expectAccessible(page);
      });
    }
  }
});

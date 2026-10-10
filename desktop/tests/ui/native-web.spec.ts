import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls } from './contracts';
import { dismissCoveringToasts, emitNewTab, emitState, installNativeWebMock, nativeCalls, seedWebTab } from './support/native-web-mock';
import design from '../../src/design/tokens.json' with { type: 'json' };

// The web tab (design 3d). The browser has no native views, so these tests use
// a typed mock of web.rs at the IPC boundary: they prove what the renderer asks
// of the native side and what it draws from the answers, never that a page
// rendered. Native isolation is proven by the Rust tests in src-tauri/src/web/.
const PANE = 'webpane1';
const URL = 'https://pkg.go.dev/encoding/json#Decoder';
const px = (name: string) => parseFloat((design.foundation as Record<string, string>)[name]);

test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

const liveSheet = (page: Page) => page.locator('.content-pane .web-sheet');

async function sheetRect(page: Page) {
  const box = (await liveSheet(page).boundingBox())!;
  return { x: Math.round(box.x), y: Math.round(box.y), width: Math.round(box.width), height: Math.round(box.height) };
}
const fillOf = (page: Page, selector: string) => page.locator(selector).evaluate(el => getComputedStyle(el).backgroundColor);
const tokenFill = (page: Page) => page.evaluate(() => {
  const probe = document.createElement('div');
  probe.style.backgroundColor = 'var(--web-sheet-fill)';
  document.body.append(probe);
  const color = getComputedStyle(probe).backgroundColor;
  probe.remove();
  return color;
});
const lastVisible = async (page: Page) => (await nativeCalls(page, 'web_visible')).at(-1)?.args.visible;

/** The last placed rectangle is the sheet's, measured in the same moment. */
async function placedOnSheet(page: Page) {
  const sent = (await nativeCalls(page, 'web_bounds')).at(-1)?.args.rect ?? (await nativeCalls(page, 'web_open'))[0]?.args.rect;
  const sheet = await sheetRect(page);
  return !!sent && sent.x === sheet.x && sent.y === sheet.y && sent.width === sheet.width && sent.height === sheet.height;
}

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
  test.beforeEach(async ({ page }) => {
    await installNativeWebMock(page);
    await seedWebTab(page);
    // The engine-down toast covers the sheet and would hide the page for the whole test.
    const go = page.goto.bind(page);
    page.goto = async (url, options) => {
      const response = await go(url, options);
      await dismissCoveringToasts(page);
      return response;
    };
  });

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
    const sheet = page.locator('.content-pane .web-sheet');
    await expect(sheet).toHaveAttribute('data-theme', 'light');
    await expect(sheet).toHaveCSS('border-top-left-radius', '8px');
    await expect(sheet).toHaveCSS('margin-top', '0px');
    await expect(sheet).toHaveCSS('margin-right', '8px');
    await expect(sheet).toHaveCSS('margin-bottom', '8px');
    await expect(sheet).toHaveCSS('margin-left', '8px');
    expect(await fillOf(page, '.content-pane .web-sheet')).toBe(await tokenFill(page));
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
    await expect(page.getByRole('button', { name: /^Address https:\/\/go\.dev\/doc/ })).not.toBeFocused();
    expect((await nativeCalls(page, 'web_navigate')).length).toBe(1);
  });

  test('anything of the app over the page hides the view and leaves the sheet blank; closing it restores the view', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: true });
    await emitState(page, PANE, { loading: false, title: 'encoding/json' });
    await expect.poll(async () => (await nativeCalls(page, 'web_snapshot')).length).toBeGreaterThan(0);
    // The overview is a modal: every web view hides under it. The sheet is blank:
    // no sentence, and the snapshot stays on the card rather than under the menu.
    await expect.poll(() => lastVisible(page)).toBe(true);
    const hides = (await nativeCalls(page, 'web_hide_all')).length;
    const shows = (await nativeCalls(page, 'web_show_all')).length;
    await page.getByRole('button', { name: /All tabs/ }).first().click();
    await expect(page.getByRole('dialog', { name: /All tabs overview/ })).toBeVisible();
    await expect.poll(() => lastVisible(page)).toBe(false);
    await expect.poll(async () => (await nativeCalls(page, 'web_hide_all')).length).toBe(hides + 1);
    const sheet = page.locator('.web-sheet');
    await expect(sheet).toHaveAttribute('data-blank', 'true');
    await expect(sheet.locator('img.web-frozen')).toHaveCount(0);
    await expect(sheet).not.toContainText(/page hidden|menu is open/i);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog', { name: /All tabs overview/ })).toHaveCount(0);
    await expect.poll(() => lastVisible(page)).toBe(true);
    await expect.poll(async () => (await nativeCalls(page, 'web_show_all')).length).toBe(shows + 1);
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

  test('a split and a collapsed rail move the one view onto the sheet', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    const before = await sheetRect(page);
    await page.locator('.workspace-tab[data-active="true"]').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Open in split' }).hover();
    await page.getByRole('menuitem', { name: 'Config stack', exact: true }).click();
    await page.mouse.move(8, 8);
    await expect(page.locator('.workspace-split-tab')).toHaveCount(1);
    await expect.poll(() => placedOnSheet(page)).toBe(true);
    const split = await sheetRect(page);
    expect(split.width).toBeLessThan(before.width);
    await page.getByRole('button', { name: 'Hide sidebar' }).click();
    await expect.poll(() => placedOnSheet(page)).toBe(true);
    expect((await sheetRect(page)).width).toBeGreaterThan(split.width);
    expect((await nativeCalls(page, 'web_open')).length).toBe(1);
    expect(await nativeCalls(page, 'web_close')).toEqual([]);
    const panes = new Set((await nativeCalls(page, 'web_open')).map(call => call.args.pane));
    expect(panes.size).toBe(1);
  });

  test('the filmstrip side card does not open a second view, and closing the overview puts the page back on the sheet', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await page.getByRole('button', { name: /All tabs/ }).first().click();
    await page.getByRole('radio', { name: 'Filmstrip' }).click();
    await page.getByRole('button', { name: 'Show Config stack' }).click();
    await expect(page.locator('.overview-film-item:not([data-cursor="true"]) .web-sheet')).toHaveCount(1);
    await expect.poll(() => lastVisible(page)).toBe(false);
    expect((await nativeCalls(page, 'web_open')).length).toBe(1);
    expect(await nativeCalls(page, 'web_close')).toEqual([]);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog', { name: /All tabs overview/ })).toHaveCount(0);
    await page.mouse.move(8, 8);
    await expect.poll(() => lastVisible(page)).toBe(true);
    await expect.poll(async () => (await nativeCalls(page, 'web_bounds')).at(-1)?.args.rect ?? (await nativeCalls(page, 'web_open'))[0].args.rect).toEqual(await sheetRect(page));
    expect((await nativeCalls(page, 'web_open')).length).toBe(1);
  });

  test('a screen scale change places the view on the sheet again, in css pixels', async ({ page, browserName }) => {
    test.skip(browserName !== 'chromium', 'screen scale is changed through Chromium emulation');
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    const before = (await nativeCalls(page, 'web_bounds')).length;
    const sheet = await sheetRect(page);
    const client = await page.context().newCDPSession(page);
    // Emulation changes the scale without the resolution event a real screen
    // move fires. The resize is the frame the view is placed on; the rectangle
    // itself does not change, so a resend is the scale, not a new size.
    await client.send('Emulation.setDeviceMetricsOverride', { width: 1200, height: 800, deviceScaleFactor: 2, mobile: false });
    await page.evaluate(() => window.dispatchEvent(new Event('resize')));
    await expect.poll(async () => (await nativeCalls(page, 'web_bounds')).length).toBeGreaterThan(before);
    const rect = (await nativeCalls(page, 'web_bounds')).at(-1)!.args.rect as { x: number; width: number };
    expect(rect).toEqual(await sheetRect(page));
    expect(rect.x).toBe(sheet.x);
    expect(rect.width).toBe(sheet.width);
    expect((await nativeCalls(page, 'web_open')).length).toBe(1);
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
    // The workspace's reaper (useWorkspaceWeb) closes the view of a pane that left the workspace.
    await expect.poll(async () => (await nativeCalls(page, 'web_close')).map(c => c.args)).toEqual([{ pane: PANE }]);
    expect(engine).toEqual([]);
  });

  test('a failed load shows why, hides the view, and Try again reloads', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await expect.poll(() => lastVisible(page)).toBe(true);
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
    await expect(page.locator('.web-address-rest')).toHaveText('Downloads do not open in web tabs');
    await emitState(page, PANE, { notice: null });
    await expect(page.locator('.web-address-rest')).toHaveText('/encoding/json#Decoder');
  });

  test("a page's new window opens a web tab in front with the workspace's host, never a window", async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitNewTab(page, PANE, 'https://go.dev/play');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(2);
    const opened = (await nativeCalls(page, 'web_open'))[1].args;
    expect(opened.url).toBe('https://go.dev/play');
    expect(opened.pane).not.toBe(PANE);
    await expect(page.locator('.workspace-tab[data-active="true"]')).toHaveAttribute('data-kind', 'web');
    await expect(page.locator('.web-address-site')).toHaveText('go.dev');
    expect(await nativeCalls(page, 'open_url')).toEqual([]);
  });

  test('with no workspace host a page\'s new window goes to the default browser, as a link chip does', async ({ page }) => {
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await page.evaluate(async () => { (await import('/src/features/web/host.ts')).setWebHost(null); });
    await emitNewTab(page, PANE, 'https://go.dev/blog');
    await expect.poll(async () => (await nativeCalls(page, 'open_url')).map(c => c.args)).toEqual([{ url: 'https://go.dev/blog' }]);
    expect((await nativeCalls(page, 'web_open')).length).toBe(1);
  });

  test('starting a conversation with the page opens an unsent draft with its picture attached once, and calls no engine', async ({ page }) => {
    const engine: string[] = [];
    await page.route('**/api/engine/**', route => { engine.push(route.request().url()); return route.abort(); });
    await page.goto('/');
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
    await emitState(page, PANE, { loading: false, title: 'encoding/json' });
    const before = (await nativeCalls(page, 'web_snapshot')).length;
    await page.getByRole('button', { name: 'Start a conversation with this page' }).click();
    const message = page.getByRole('textbox', { name: 'Message', exact: true });
    await expect(message).toHaveValue(`About this page, "encoding/json": ${URL}\n\n`);
    await expect(page.getByRole('img', { name: 'page.png' })).toHaveCount(1);
    expect((await nativeCalls(page, 'web_snapshot')).length).toBe(before + 1);
    // Leaving and returning does not attach it a second time.
    await page.locator('.workspace-tab[data-kind="web"] .workspace-tab-select').click();
    await page.locator('.workspace-tab[data-kind="conversation"] .workspace-tab-select').last().click();
    await expect(page.getByRole('img', { name: 'page.png' })).toHaveCount(0);
    expect(engine.filter(url => /\/turn|\/session/.test(url) && !/\/sessions?$/.test(url))).toEqual([]);
  });

  for (const theme of ['light', 'dark'] as const) {
    for (const width of [320, 800, 1200]) {
      test(`${theme} at ${width}px: header controls stay reachable without overflow and pass accessibility`, async ({ page }) => {
        await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), theme);
        await page.setViewportSize({ width, height: width < 800 ? 560 : 800 });
        await page.goto('/');
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await expect(liveSheet(page)).toHaveAttribute('data-theme', 'light');
        expect(await fillOf(page, '.content-pane .web-sheet')).toBe(await tokenFill(page));
        await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
        await emitState(page, PANE, { loading: false, title: 'encoding/json', canBack: true });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        const header = (await page.locator('.web-header').boundingBox())!;
        for (const name of ['Back', 'Forward', 'Reload', width <= 600 ? 'Page actions' : 'Open in browser']) {
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

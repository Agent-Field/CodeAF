import { test, expect, type Locator, type Page } from '@playwright/test';
import { expectAccessible, tokenColor } from './contracts';
import { desktopTabEvent } from '../../src/lib/desktopMenuRoute';
import { refusalSentence } from '../../src/features/web/address';
import { dismissCoveringToasts, emitState, installNativeWebMock, nativeCalls, seedWebTab } from './support/native-web-mock';
import design from '../../src/design/tokens.json' with { type: 'json' };

// Gaps the native-web suite (t-d5nw) does not measure. Sibling specs already lock
// geometry, URL keys in the field, bounds, occlusion, favicon, the first load
// line, new-window, chat-plus, link chips, find, and the pasted-URL row.
// This file locks what those leave open: the two address inks, Forward at
// ink-3, the menu and keyboard chords, every remaining error sentence, the
// text preview when there is no picture, and Light + Dark at 320 and 600.
//
// Manual Tauri smoke — the integrator runs this on macOS and on Linux with the
// real app. Playwright replaces the child webview with a recorder, so a green
// run here does not show a page. Record pass or fail for each line:
// 1. Open https://example.com. The page sits in the card under the 48px row, on a light sheet, in both Light and Dark.
// 2. Resize the window, then open the tab in a split. The page follows the sheet both times.
// 3. Switch tabs, and open a menu over the web tab. The page hides each time and comes back when the tab is shown and the menu is gone.
// 4. The tab title is the page title. A favicon shows when the site has one; otherwise the monogram.
// 5. With the page focused: Cmd/Ctrl+L selects the address, Cmd/Ctrl+R reloads, Cmd/Ctrl+F finds, Cmd/Ctrl+T opens a new tab, Cmd/Ctrl+W closes the web tab.
// 6. A target=_blank link opens one background web tab after this one, and no second window.
// 7. Quit and relaunch. The web tab is back on the same address.

const PANE = 'webpane1';
const URL = 'https://pkg.go.dev/encoding/json#Decoder';
const px = (name: string) => parseFloat((design.foundation as Record<string, string>)[name]);
const notices = {
  blocked: 'That page tried to open something a web tab does not open',
  permission: 'Web tabs do not get the camera, microphone or location',
} as const;

async function boot(page: Page, theme: 'light' | 'dark', width = 1200, snapshot = true) {
  await page.route('**/api/engine/**', route => route.abort());
  await installNativeWebMock(page, { snapshot });
  await seedWebTab(page);
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.setViewportSize({ width, height: 800 });
  await page.goto('/');
  await dismissCoveringToasts(page);
  await expect.poll(async () => (await nativeCalls(page, 'web_open')).length).toBe(1);
  await emitState(page, PANE, { loading: false, title: 'encoding/json', canBack: true, canForward: false });
}

const ringOf = (page: Page) => page.evaluate(() => {
  const probe = document.createElement('div');
  probe.style.boxShadow = '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 var(--focus-halo-width) var(--accent-soft)';
  document.body.append(probe);
  const value = getComputedStyle(probe).boxShadow;
  probe.remove();
  return value;
});

async function tabTo(page: Page, target: Locator) {
  for (let i = 0; i < 50 && !await target.evaluate(el => el === document.activeElement); i++) await page.keyboard.press('Tab');
  await expect(target).toBeFocused();
}

test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: the 48px row paints the host in ink, the path in ink-3, and a disabled Forward in ink-3`, async ({ page }) => {
    await boot(page, theme);
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    const header = page.locator('.web-header');
    expect(await header.evaluate(el => el.getBoundingClientRect().height)).toBe(px('web-header-height'));
    await expect(page.locator('.web-address-site')).toHaveCSS('color', await tokenColor(page, 'ink'));
    await expect(page.locator('.web-address-rest')).toHaveCSS('color', await tokenColor(page, 'ink-3'));
    const forward = page.getByRole('button', { name: 'Forward', exact: true });
    await expect(forward).toBeDisabled();
    await expect(forward).toHaveCSS('color', await tokenColor(page, 'ink-3'));
  });

  for (const width of [320, 600]) {
    test(`${theme} at ${width}px: no overflow, the sheet stays light, and keyboard focus draws the ring`, async ({ page }) => {
      test.setTimeout(60_000);
      await boot(page, theme, width);
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      const sheet = page.locator('.content-pane .web-sheet');
      await expect(sheet).toHaveAttribute('data-theme', 'light');
      await expect(sheet).toHaveCSS('background-color', await page.evaluate(() => {
        const probe = document.createElement('div');
        probe.style.backgroundColor = 'var(--web-sheet-fill)';
        document.body.append(probe);
        const color = getComputedStyle(probe).backgroundColor;
        probe.remove();
        return color;
      }));
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
      const header = page.locator('.web-header');
      const headerBox = (await header.boundingBox())!;
      for (const name of ['Back', 'Reload', 'Page actions']) {
        const box = (await header.getByRole('button', { name, exact: true }).boundingBox())!;
        expect(box.x).toBeGreaterThanOrEqual(headerBox.x - 0.5);
        expect(box.x + box.width).toBeLessThanOrEqual(headerBox.x + headerBox.width + 0.5);
      }
      const back = header.getByRole('button', { name: 'Back', exact: true });
      await tabTo(page, back);
      await expect(back).toHaveCSS('box-shadow', await ringOf(page));
      const reached: string[] = [];
      for (let i = 0; i < 6; i++) {
        const label = await page.evaluate(() => {
          const el = document.activeElement;
          return el?.closest('.web-header') ? el.getAttribute('aria-label') : '';
        });
        if (!label) break;
        reached.push(label);
        await page.keyboard.press('Tab');
      }
      expect(reached).toEqual(['Back', 'Reload', `Address ${URL}. Edit address`, 'Page actions']);
      await back.click();
      await expect(back).toHaveCSS('box-shadow', 'none');
      if (width === 600) await expectAccessible(page);
    });
  }
}

test('menu and keyboard chords focus the address, reload, open a tab, and close the web tab', async ({ page }) => {
  await boot(page, 'light');
  const send = (detail: string) => page.evaluate(([name, id]) => window.dispatchEvent(new CustomEvent(name, { detail: id })), [desktopTabEvent, detail] as const);
  const input = () => page.getByRole('textbox', { name: 'Address', exact: true });
  const modifier = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');

  await send('web-address');
  await expect(input()).toBeFocused();
  await expect.poll(() => input().evaluate((el: HTMLInputElement) => [el.selectionStart, el.selectionEnd])).toEqual([0, URL.length]);
  await page.keyboard.press('Escape');
  await expect(input()).toHaveCount(0);

  await send('web-reload');
  await expect.poll(async () => (await nativeCalls(page, 'web_history')).at(-1)?.args.step).toBe('reload');

  await page.keyboard.press(`${modifier}+l`);
  await expect(input()).toBeFocused();
  await page.keyboard.press('Escape');
  await page.keyboard.press(`${modifier}+r`);
  await expect.poll(async () => (await nativeCalls(page, 'web_history')).filter(call => call.args.step === 'reload').length).toBe(2);

  await send('new');
  await expect(page.locator('.workspace-tab[data-kind="newtab"][data-active="true"]')).toBeVisible();
  await expect(page.locator('.workspace-tab[data-kind="web"]')).toHaveCount(1);
  await expect.poll(async () => (await nativeCalls(page, 'web_visible')).at(-1)?.args.visible).toBe(false);

  await page.locator('.workspace-tab[data-kind="web"] .workspace-tab-select').click();
  await expect.poll(async () => (await nativeCalls(page, 'web_visible')).at(-1)?.args.visible).toBe(true);
  await page.keyboard.press(`${modifier}+w`);
  await expect(page.locator('.workspace-tab[data-kind="web"]')).toHaveCount(0);
  await expect.poll(async () => (await nativeCalls(page, 'web_close')).map(call => call.args)).toEqual([{ pane: PANE }]);
});

test('every refusal and the remaining notices say their line, and Try again restores the 2px load line', async ({ page }) => {
  await boot(page, 'light');
  const alert = page.locator('.web-state[role="alert"]');
  const tab = page.locator('.workspace-tab[data-kind="web"]');
  const danger = await tokenColor(page, 'danger');
  for (const reason of ['tooLong', 'malformed', 'scheme', 'noHost', 'credentials'] as const) {
    await emitState(page, PANE, { loading: false, failure: { kind: 'refused', reason } });
    await expect(alert).toContainText('This address does not open here');
    await expect(alert).toContainText(`${refusalSentence[reason]}.`);
    await expect(tab).toHaveAttribute('data-state', 'failed');
    await expect(tab.locator('[data-state="waiting"]')).toHaveCount(0);
    expect(await alert.locator('p').first().evaluate(el => getComputedStyle(el).color)).not.toBe(danger);
  }
  await alert.getByRole('button', { name: 'Try again' }).click();
  await expect(alert).toHaveCount(0);
  const loading = page.getByRole('progressbar', { name: 'Loading page' });
  await expect(loading).toBeVisible();
  expect(await loading.evaluate(el => getComputedStyle(el).height)).toBe(design.foundation['tab-load-height']);
  await expect.poll(async () => (await nativeCalls(page, 'web_history')).at(-1)?.args.step).toBe('reload');
  await emitState(page, PANE, { loading: false, failure: null, title: 'encoding/json' });
  await expect(loading).toHaveCount(0);
  await expect(page.locator('.web-address-site')).toHaveText('pkg.go.dev');
  await expect(page.locator('.web-address-rest')).toHaveText('/encoding/json#Decoder');
  await expect(tab).not.toHaveAttribute('data-state', 'failed');

  for (const [notice, line] of Object.entries(notices)) {
    await emitState(page, PANE, { loading: false, failure: null, notice });
    await expect(page.locator('.web-address-rest')).toHaveText(line);
  }
  await emitState(page, PANE, { notice: null });
  await expect(page.locator('.web-address-rest')).toHaveText('/encoding/json#Decoder');
});

test('a web tab with no picture previews as title, address, and the honest note', async ({ page }) => {
  await boot(page, 'dark', 1200, false);
  await expect(page.locator('.workspace-tab[data-kind="web"]')).toContainText('encoding/json');
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await page.locator('.workspace-tab[data-kind="web"] .workspace-tab-select').hover();
  const card = page.getByRole('group', { name: 'Preview of encoding/json', exact: true });
  await expect(card).toBeVisible();
  await expect(card.locator('.preview-title')).toHaveText('encoding/json');
  await expect(card.locator('.preview-address')).toHaveText('pkg.go.dev/encoding/json');
  await expect(card.locator('.preview-shot-note')).toHaveText('No picture of this page here');
  await expect(card.locator('img')).toHaveCount(0);
});

import { test, expect, type Page, type Route } from '@playwright/test';
import { expectAccessible, expectNoUnstyledControls, tokenColor } from './contracts';
import { openApp, expectNoHorizontalOverflow } from './support/conversation';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openPage } from './support/shell-navigation';

const decoy = 'settings-test-decoy-value-do-not-render';
const engine = (page: Page) => page.getByRole('region', { name: 'Engine', exact: true });
const retry = (page: Page) => engine(page).getByRole('button', { name: 'Retry', exact: true });

async function chooseTheme(page: Page, theme: 'light' | 'dark' | 'system') {
  await page.getByRole('combobox', { name: 'Theme', exact: true }).click();
  await page.getByRole('option', { name: `${theme[0].toUpperCase()}${theme.slice(1)} appearance`, exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
}

async function openSettings(page: Page) {
  await openPage(page, 'Settings');
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Provider key', exact: true })).toBeVisible();
}

async function expectThemeChoice(page: Page, label: string) {
  await page.getByRole('combobox', { name: 'Theme', exact: true }).click();
  await expect(page.getByRole('option', { name: label, exact: true })).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Escape');
}

for (const theme of ['light', 'dark'] as const) {
  test(`TS-04/05: key status is words only; appearance applies and persists · ${theme}`, async ({ page }) => {
    await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
    await page.route('**/settings/key', route => route.fulfill({ json: { present: true, source: 'OPENROUTER_API_KEY', value: decoy } }));
    // An opposite system preference proves that the explicit choice controls the appearance.
    await page.emulateMedia({ colorScheme: theme === 'light' ? 'dark' : 'light', reducedMotion: 'reduce' });
    await openApp(page);
    await openSettings(page);
    const key = page.getByRole('region', { name: 'Provider key', exact: true });
    await expect(key.locator('[data-setting="provider-key"]')).toHaveText('Provider keyFrom OPENROUTER_API_KEY');
    // Hidden attributes and input values are still DOM disclosure, even when getByText cannot find them.
    expect(await page.content()).not.toContain(decoy);
    await expect(key.locator('input, textarea, button')).toHaveCount(0);
    const appearance = page.getByRole('region', { name: 'Appearance', exact: true });
    await expect(appearance).toContainText('Reduce motion follows your system setting.');
    await expect(appearance.getByRole('checkbox')).toHaveCount(0);
    await expect(appearance.getByRole('switch')).toHaveCount(0);

    const systemInk = await page.locator('.settings-page').evaluate(node => getComputedStyle(node).color);
    await chooseTheme(page, theme);
    await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
    await expect(page.locator('.settings-page')).toHaveCSS('color', await tokenColor(page, 'ink'));
    const before = await page.locator('.settings-page').evaluate(node => getComputedStyle(node).color);
    expect(before).not.toBe(systemInk);
    await page.reload();
    await openSettings(page);
    await expectThemeChoice(page, `${theme === 'light' ? 'Light' : 'Dark'} appearance`);
    await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
    await expect(page.locator('.settings-page')).toHaveCSS('color', before);
    expect(await page.content()).not.toContain(decoy);

    await chooseTheme(page, 'system');
    await page.emulateMedia({ colorScheme: theme });
    await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
    await page.reload();
    await openSettings(page);
    await expectThemeChoice(page, 'System appearance');
    await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
  });

  test(`TS-06/12: Retry states, keyboard order, 320px and axe · ${theme}`, async ({ page }) => {
    await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
    await page.route('**/settings/key', route => route.fulfill({ json: { present: false, value: decoy } }));
    let state: 'unreachable' | 'held' | 'connected' = 'unreachable';
    let pending: Route | undefined;
    let release: (() => void) | undefined;
    const gate = new Promise<void>(resolve => { release = resolve; });
    await page.route('**/api/engine/health', async route => {
      if (state === 'unreachable') return route.abort('connectionrefused');
      if (state === 'held') { pending = route; await gate; }
      await route.fulfill({ json: { status: 'ready', version: 'dev', platform: 'browser' } });
    });
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await openApp(page);
    await openSettings(page);
    await chooseTheme(page, theme);
    await expect(page.locator('[data-setting="provider-key"]')).toHaveText('Provider keyNot set');
    expect(await page.content()).not.toContain(decoy);
    const status = engine(page).getByRole('status');
    await expect(status).toHaveText("Can't reach the engine");
    await expect(status).toHaveCSS('color', await tokenColor(page, 'ink-2'));
    await expect(retry(page)).toBeVisible();

    // Models precede the informational key row, then Theme and Retry; the key row adds no tab stop.
    const modelControls = page.locator('.settings-page button, .settings-page input').filter({ visible: true });
    const beforeTheme = await modelControls.evaluateAll(nodes => {
      const themeControl = document.querySelector('[role="combobox"][aria-label="Theme"]');
      return nodes.filter(node => (node as HTMLElement).tabIndex >= 0 && !(node as HTMLButtonElement).disabled
        && themeControl && Boolean(node.compareDocumentPosition(themeControl) & Node.DOCUMENT_POSITION_FOLLOWING))
        .map(node => node.getAttribute('aria-label'));
    });
    expect(beforeTheme.length).toBeGreaterThan(0);
    expect(beforeTheme.at(-1)).toBeTruthy();
    const themeControl = page.getByRole('combobox', { name: 'Theme' });
    await themeControl.focus();
    await page.keyboard.press('Shift+Tab');
    await expect(page.getByLabel(beforeTheme.at(-1)!, { exact: true })).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(themeControl).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('listbox')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(themeControl).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(retry(page)).toBeFocused();
    await expectAccessible(page);
    await expectNoUnstyledControls(page);

    await page.setViewportSize({ width: 320, height: 700 });
    await expectNoHorizontalOverflow(page);
    for (const name of ['Provider key', 'Appearance', 'Engine']) {
      const box = await page.getByRole('region', { name, exact: true }).boundingBox();
      expect(box).not.toBeNull();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(320);
    }
    await expectAccessible(page);
    await retry(page).scrollIntoViewIfNeeded();
    await expect(retry(page)).toBeInViewport();

    // Holding the response proves Retry disappears during an actual pending attempt, without a timing sleep.
    state = 'held';
    await retry(page).click();
    await expect.poll(() => Boolean(pending)).toBe(true);
    await expect(status).toHaveText('Reconnecting…');
    await expect(retry(page)).toHaveCount(0);
    state = 'connected';
    release!();
    await expect(status).toHaveText('Connected');
    await expect(retry(page)).toHaveCount(0);
    await expect(status).toHaveCSS('color', await tokenColor(page, 'ink-2'));
    await expectNoHorizontalOverflow(page);
  });
}

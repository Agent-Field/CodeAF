import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openApp, send } from './support/conversation';
import { installNativeWebMock, nativeCalls } from './support/native-web-mock';
import design from '../../src/design/tokens.json' with { type: 'json' };

const URL = 'https://go.dev/blog/errors?view=full#wrapping';
const chip = (page: Page) => page.locator('.link-chip').first();

async function links(page: Page, native = false, platform?: string) {
  if (platform) await page.addInitScript(value => Object.defineProperty(navigator, 'platform', { value }), platform);
  if (native) await installNativeWebMock(page, { engine: true });
  await page.addInitScript(() => {
    const copied: string[] = [];
    const opened: string[] = [];
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: async (value: string) => { copied.push(value); } } });
    window.open = ((url: string) => { opened.push(url); return null; }) as typeof window.open;
    Object.assign(window, { __linkChip: { copied, opened } });
  });
  await installMockEngine(page, {
    initial: { title: '', entries: [] },
    turns: [{ entries: [{ Role: 'assistant', Text: `${URL}\n\n[Read the article](${URL})`, Answer: true }] }],
  });
  await openApp(page);
  await send(page, 'Show the article');
  await expect(chip(page)).toBeVisible();
}

const receipt = (page: Page, kind: 'copied' | 'opened') => page.evaluate(key =>
  (window as unknown as { __linkChip: { copied: string[]; opened: string[] } }).__linkChip[key], kind);

for (const platform of ['MacIntel', 'Linux x86_64']) {
  for (const gesture of ['modifier', 'middle'] as const) {
    test(`${platform}: ${gesture} opens a real workspace web tab through the native adapter`, async ({ page }) => {
      await links(page, true, platform);
      await chip(page).click(gesture === 'middle' ? { button: 'middle' } : { modifiers: [platform === 'MacIntel' ? 'Meta' : 'Control'] });
      await expect.poll(async () => (await nativeCalls(page, 'web_open')).map(call => call.args.url)).toEqual([URL]);
      await expect(page.locator('.content-pane .web-sheet')).toBeVisible();
      expect(await nativeCalls(page, 'open_url')).toEqual([]);
    });
  }
}

test('plain desktop click opens the default browser without creating a web tab', async ({ page }) => {
  await links(page, true);
  await chip(page).click();
  await expect.poll(async () => (await nativeCalls(page, 'open_url')).map(call => call.args.url)).toEqual([URL]);
  expect(await nativeCalls(page, 'web_open')).toEqual([]);
});

test('browser builds route plain, modifier and middle clicks to the default browser', async ({ page }) => {
  await links(page);
  const primary = await page.evaluate(() => /Mac/.test(navigator.platform)) ? 'Meta' : 'Control';
  await chip(page).click();
  await chip(page).click({ modifiers: [primary] });
  await chip(page).click({ button: 'middle' });
  await expect.poll(() => receipt(page, 'opened')).toEqual([URL, URL, URL]);
  await expect(page.locator('.web-sheet')).toHaveCount(0);
});

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: right-click and keyboard menus copy the full URL without opening it`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await links(page);
    await chip(page).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Copy link', exact: true }).click();
    await expect.poll(() => receipt(page, 'copied')).toEqual([URL]);
    await chip(page).focus();
    await page.keyboard.press('Shift+F10');
    await expect(page.getByRole('menuitem', { name: 'Copy link', exact: true })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect.poll(() => receipt(page, 'copied')).toEqual([URL, URL]);
    expect(await receipt(page, 'opened')).toEqual([]);
    await expect(chip(page)).toBeFocused();
  });

  test(`${theme}: shared full-URL tooltip waits 500ms and dismisses on Escape and press`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await links(page);
    await expect(chip(page)).not.toHaveAttribute('title');
    await page.mouse.move(0, 0);
    await page.clock.install();
    await page.clock.pauseAt(new Date(await page.evaluate(() => Date.now()) + 1000));
    const bounds = (await chip(page).boundingBox())!;
    await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2);
    await page.clock.runFor(design.interaction.tooltipOpenDelay - 1);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await page.clock.runFor(1);
    await expect(page.getByRole('tooltip')).toHaveText(URL);
    expect(await chip(page).evaluate(e => e.getBoundingClientRect().height)).toBe(parseFloat(design.foundation['chip-height']));
    expect(await page.getByRole('tooltip').evaluate(e => getComputedStyle(e).fontSize)).toBe(design.foundation['type-caption-size']);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await page.mouse.move(0, 0);
    await chip(page).hover();
    await page.clock.runFor(design.interaction.tooltipOpenDelay);
    await expect(page.getByRole('tooltip')).toHaveText(URL);
    await chip(page).click({ button: 'right' });
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await expect(page.getByRole('menuitem', { name: 'Copy link', exact: true })).toBeVisible();
  });
}

test('prose links share the URL menu and web-tab gesture', async ({ page }) => {
  await links(page, true);
  const prose = page.getByRole('link', { name: 'Read the article' });
  await expect(prose).not.toHaveAttribute('title');
  await prose.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Copy link', exact: true }).click();
  await expect.poll(() => receipt(page, 'copied')).toEqual([URL]);
  await prose.click({ button: 'middle' });
  await expect.poll(async () => (await nativeCalls(page, 'web_open')).map(call => call.args.url)).toEqual([URL]);
});

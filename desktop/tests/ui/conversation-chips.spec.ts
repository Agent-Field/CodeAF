import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { expectNoHorizontalOverflow, message, openApp, send } from './support/conversation';
import { installNativeWebMock, nativeCalls } from './support/native-web-mock';
import { expectAccessible } from './contracts';
import design from '../../src/design/tokens.json' with { type: 'json' };

// CV-083..CV-088. File and link chips in a real answer: the menu, both copies,
// the 500ms designer tooltip, the browser, and a web tab. Removing any of those
// fails this file.

const FILE = 'internal/payments/providers/stripe/webhooks/handler_test.go';
const DIR = 'internal/payments/providers/stripe/webhooks';
const ABS = `/mock-workspace/${FILE}`;
const URL = 'https://go.dev/blog/errors?view=full#wrapping';
const delay = design.interaction.tooltipOpenDelay;

test.beforeEach(({}, testInfo) => {
  testInfo.setTimeout(90_000);
});

const fileChip = (page: Page) => page.locator('.answer-block .file-chip');
const linkChip = (page: Page) => page.locator('.answer-block .link-chip');
const copied = (page: Page) => page.evaluate(() => (window as unknown as { __chipCopies: string[] }).__chipCopies);
const opened = (page: Page) => page.evaluate(() => (window as unknown as { __chipOpens: string[] }).__chipOpens);
const revealLabel = (platform: string) => (/Mac/.test(platform) ? 'Reveal in Finder' : 'Reveal in Files');

function chipsReply() {
  return {
    initial: { title: 'Chips', entries: [] },
    turns: [{ entries: [{ Role: 'assistant' as const, Text: `Edited \`${FILE}\`.\n\n${URL}`, Answer: true }] }],
    files: { [FILE]: { mime: 'text/plain', dataBase64: Buffer.from('package webhooks\n').toString('base64') } },
  };
}

async function openChips(page: Page, options: { native?: boolean; platform?: string; theme: 'light' | 'dark'; width?: number }) {
  await page.emulateMedia({ colorScheme: options.theme });
  if (options.width) await page.setViewportSize({ width: options.width, height: 800 });
  await page.addInitScript(platform => {
    if (platform) Object.defineProperty(navigator, 'platform', { value: platform });
    const copies: string[] = [];
    const opens: string[] = [];
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (value: string) => { copies.push(value); } } });
    window.open = ((url?: string) => { if (url) opens.push(url); return null; }) as typeof window.open;
    Object.assign(window, { __chipCopies: copies, __chipOpens: opens });
  }, options.platform ?? '');
  if (options.native) await installNativeWebMock(page, { engine: true });
  await installMockEngine(page, chipsReply());
  await openApp(page);
  await send(page, 'Show the chips');
  await expect(fileChip(page)).toBeVisible();
  await expect(linkChip(page)).toHaveAttribute('href', URL);
  await expect(fileChip(page)).not.toHaveAttribute('title');
  await expect(linkChip(page)).not.toHaveAttribute('title');
}

async function freeze(page: Page) {
  await page.clock.install();
  await page.clock.pauseAt(new Date(await page.evaluate(() => Date.now()) + 1000));
}

/** Caller has already frozen the clock and left the pointer off every chip. */
async function expectHoverTooltip(page: Page, target: Locator, text: string) {
  await hover(page, target);
  await page.clock.runFor(delay - 1);
  await expect(page.getByRole('tooltip')).toHaveCount(0);
  await page.clock.runFor(1);
  await expect(page.getByRole('tooltip')).toHaveText(text);
  expect(await page.getByRole('tooltip').evaluate(element => getComputedStyle(element).fontSize)).toBe(design.foundation['type-caption-size']);
}

async function hover(page: Page, target: Locator) {
  const box = (await target.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
}

async function activate(page: Page, name: string) {
  const item = page.getByRole('menuitem', { name, exact: true });
  await expect(item).toBeVisible();
  for (let step = 0; step < 8; step += 1) {
    if (await item.evaluate(node => node === document.activeElement)) break;
    await page.keyboard.press('ArrowDown');
  }
  await expect(item).toBeFocused();
  await page.keyboard.press('Enter');
}

async function settledBox(target: Locator) {
  return target.evaluate(element => {
    const box = element.getBoundingClientRect();
    return { x: box.x, y: box.y, width: box.width, height: box.height };
  });
}

function boxInside(box: { x: number; y: number; width: number; height: number }, viewport: { width: number; height: number }) {
  return box.x >= -1 && box.y >= -1 && box.x + box.width <= viewport.width + 1 && box.y + box.height <= viewport.height + 1;
}

async function expectInside(page: Page, target: Locator) {
  const viewport = page.viewportSize()!;
  const box = await settledBox(target);
  expect(boxInside(box, viewport), JSON.stringify(box)).toBe(true);
}

/** A context menu stays on the pointer, so at 320px its right edge can leave the window. The item's centre stays inside, which is what a click hits. */
async function expectReachable(page: Page, target: Locator) {
  const viewport = page.viewportSize()!;
  const box = await settledBox(target);
  const centerX = box.x + box.width / 2;
  const centerY = box.y + box.height / 2;
  expect(centerX, JSON.stringify(box)).toBeGreaterThanOrEqual(0);
  expect(centerX, JSON.stringify(box)).toBeLessThanOrEqual(viewport.width);
  expect(centerY, JSON.stringify(box)).toBeGreaterThanOrEqual(0);
  expect(centerY, JSON.stringify(box)).toBeLessThanOrEqual(viewport.height);
}

test.beforeAll(() => {
  expect(delay).toBe(500);
});

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: a file chip menu copies the absolute path and the workspace path`, async ({ page }) => {
    const platform = 'Linux x86_64';
    await openChips(page, { native: true, platform, theme });
    await expect(fileChip(page)).toHaveAccessibleDescription(FILE);
    await expect(fileChip(page).locator('.file-chip-dir')).toContainText('…');
    await expect(fileChip(page).locator('.file-chip-dir')).not.toHaveText(DIR);
    expect(await fileChip(page).evaluate(element => element.getBoundingClientRect().height)).toBe(parseFloat(design.foundation['chip-height']));

    await fileChip(page).click({ button: 'right' });
    await expect(page.getByRole('dialog', { name: /Preview/ })).toHaveCount(0);
    const menu = page.getByRole('menu', { name: 'Actions for handler_test.go' });
    await expect(menu.getByRole('menuitem')).toHaveText(['Open in editor', revealLabel(platform), 'Copy path', 'Copy relative path']);
    for (const name of ['Open in editor', revealLabel(platform), 'Copy path', 'Copy relative path']) {
      await expect(menu.getByRole('menuitem', { name, exact: true })).toBeEnabled();
    }
    await expectAccessible(page);

    await page.getByRole('menuitem', { name: 'Open in editor', exact: true }).click();
    await expect.poll(async () => (await nativeCalls(page, 'open_path')).map(call => call.args.path)).toEqual([ABS]);
    await fileChip(page).click({ button: 'right' });
    await page.getByRole('menuitem', { name: revealLabel(platform), exact: true }).click();
    await expect.poll(async () => (await nativeCalls(page, 'reveal_path')).map(call => call.args.path)).toEqual([ABS]);

    await fileChip(page).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Copy path', exact: true }).click();
    await expect.poll(() => copied(page)).toEqual([ABS]);
    await fileChip(page).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Copy relative path', exact: true }).click();
    await expect.poll(() => copied(page)).toEqual([ABS, FILE]);

    await fileChip(page).focus();
    await page.keyboard.press('Shift+F10');
    await activate(page, 'Copy relative path');
    await expect.poll(() => copied(page)).toEqual([ABS, FILE, FILE]);
    await expect(fileChip(page)).toBeFocused();
    expect(await nativeCalls(page, 'open_url')).toEqual([]);
  });

  test(`${theme}: a link chip opens the browser, a web tab, or copies the URL`, async ({ page }) => {
    await openChips(page, { native: true, platform: 'Linux x86_64', theme });
    await linkChip(page).click();
    await expect.poll(async () => (await nativeCalls(page, 'open_url')).map(call => call.args.url)).toEqual([URL]);
    expect(await nativeCalls(page, 'web_open')).toEqual([]);
    expect(await opened(page)).toEqual([]);

    await linkChip(page).click({ modifiers: ['Control'] });
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).map(call => call.args.url)).toEqual([URL]);
    await expect(page.locator('.content-pane .web-sheet')).toBeVisible();
    expect(await nativeCalls(page, 'open_url')).toHaveLength(1);

    await page.getByRole('tab').first().click();
    await expect(linkChip(page)).toBeVisible();
    await linkChip(page).click({ button: 'middle' });
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).map(call => call.args.url)).toEqual([URL, URL]);
    expect(await opened(page)).toEqual([]);

    await page.getByRole('tab').first().click();
    await expect(linkChip(page)).toBeVisible();
    await linkChip(page).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Copy link', exact: true }).click();
    await expect.poll(() => copied(page)).toEqual([URL]);
    await linkChip(page).focus();
    await page.keyboard.press('Shift+F10');
    await expect(page.getByRole('menuitem', { name: 'Copy link', exact: true })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect.poll(() => copied(page)).toEqual([URL, URL]);
    expect(await opened(page)).toEqual([]);
    await expect(linkChip(page)).toBeFocused();
  });

  test(`${theme} Mac: Reveal in Finder, and Command-click opens a web tab`, async ({ page }) => {
    await openChips(page, { native: true, platform: 'MacIntel', theme });
    await fileChip(page).click({ button: 'right' });
    await expect(page.getByRole('menuitem', { name: 'Reveal in Finder', exact: true })).toBeEnabled();
    await expect(page.getByRole('menuitem', { name: 'Reveal in Files' })).toHaveCount(0);
    await page.keyboard.press('Escape');

    await linkChip(page).click({ modifiers: ['Control'] });
    expect(await nativeCalls(page, 'web_open')).toEqual([]);
    expect(await nativeCalls(page, 'open_url')).toEqual([]);
    await linkChip(page).click({ modifiers: ['Meta'] });
    await expect.poll(async () => (await nativeCalls(page, 'web_open')).map(call => call.args.url)).toEqual([URL]);
    await expect(page.locator('.content-pane .web-sheet')).toBeVisible();
    expect(await nativeCalls(page, 'open_url')).toEqual([]);
  });

  test(`${theme}: without a web tab, every link gesture opens the browser`, async ({ page }) => {
    await openChips(page, { platform: 'Linux x86_64', theme });
    await linkChip(page).click();
    await linkChip(page).click({ modifiers: ['Control'] });
    await linkChip(page).click({ button: 'middle' });
    await expect.poll(() => opened(page)).toEqual([URL, URL, URL]);
    await expect(page.locator('.web-sheet')).toHaveCount(0);
    await fileChip(page).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Copy path', exact: true }).click();
    await expect.poll(() => copied(page)).toEqual([ABS]);
  });

  for (const width of [320, 600, 1200]) {
    test(`${theme} at ${width}px: the chip menu and the full-path tooltip stay on screen`, async ({ page }) => {
      await openChips(page, { native: true, platform: 'Linux x86_64', theme, width });
      await expectNoHorizontalOverflow(page);
      await fileChip(page).click({ button: 'right' });
      const menu = page.getByRole('menu', { name: 'Actions for handler_test.go' });
      await expect(menu).toBeVisible();
      if (width === 320) {
        for (const name of ['Open in editor', 'Reveal in Files', 'Copy path', 'Copy relative path']) {
          await expectReachable(page, menu.getByRole('menuitem', { name, exact: true }));
        }
        // Axe walks the page on timers. The tooltip check below freezes the clock, so the scan has to run first.
        await expectAccessible(page);
      } else {
        await expectInside(page, menu);
      }
      await page.keyboard.press('Escape');
      await expect(menu).toHaveCount(0);
      await expect(fileChip(page)).toBeFocused();
      await page.mouse.move(0, 0);
      await freeze(page);
      await expectHoverTooltip(page, fileChip(page), FILE);
      const tip = page.getByRole('tooltip', { name: FILE });
      await expectInside(page, tip);
    });
  }

  test(`${theme}: file and link tooltips wait 500ms and dismiss`, async ({ page }) => {
    await openChips(page, { native: true, platform: 'Linux x86_64', theme });
    await page.mouse.move(0, 0);
    await freeze(page);
    await message(page).focus();
    for (let step = 0; step < 16 && !(await fileChip(page).evaluate(element => element === document.activeElement)); step += 1) {
      await page.keyboard.press('Shift+Tab');
    }
    await expect(fileChip(page)).toBeFocused();
    await page.clock.runFor(delay - 1);
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await page.clock.runFor(1);
    await expect(page.getByRole('tooltip')).toHaveText(FILE);
    await page.keyboard.press('Escape');
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await page.mouse.move(0, 0);
    await page.clock.runFor(design.interaction.previewCloseDelay + 1);
    await expectHoverTooltip(page, linkChip(page), URL);
    await linkChip(page).click({ button: 'right' });
    await expect(page.getByRole('tooltip')).toHaveCount(0);
    await expect(page.getByRole('menuitem', { name: 'Copy link', exact: true })).toBeVisible();
  });
}

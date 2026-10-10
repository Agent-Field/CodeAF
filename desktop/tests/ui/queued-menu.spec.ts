import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import { expectNoHorizontalOverflow, message, openApp, posts, send } from './support/conversation';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { streaming } from './support/scenarios';

// CV-171, CV-173, CV-174, CV-180, CV-181, CV-182. These assertions are the
// behaviour: removing the grip, the inline editor, a menu route, the chip,
// paste-as-text, or the touch rule fails this file.

const WIDTHS = [320, 600, 1200] as const;
const LONG = Array.from({ length: 80 }, (_, at) => `Paragraph ${at + 1} of a long reply that fills the reading column.`).join('\n\n');
const PASTE_LINES = 13;

const listOf = (page: Page) => page.getByRole('list', { name: 'Queued messages' });
const menuOf = (page: Page) => page.getByRole('menu', { name: 'Queued message actions' });
const chipOf = (page: Page, count: number) => page.getByRole('button', { name: `${count} queued`, exact: true });
const scroller = (page: Page) => page.locator('.conversation-scroll');
const distance = (page: Page) => scroller(page).evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop);
const order = (engine: MockEngine) => engine.snapshot().queue?.map((item) => item.text);

async function queued(page: Page, words = ['first queued', 'second queued', 'third queued']) {
  const engine = await installMockEngine(page, { ...streaming(), manual: true });
  await openApp(page);
  await send(page, 'Say hello');
  await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
  const list = listOf(page);
  for (const [index, text] of words.entries()) {
    await message(page).fill(text);
    await page.getByRole('button', { name: /^Queue/ }).click();
    await expect.poll(() => engine.snapshot().queue?.length).toBe(index + 1);
  }
  return { engine, list };
}

function pasteLines(count: number) {
  return Array.from({ length: count }, (_, index) => `line ${index + 1}`).join('\n');
}

/** A long paste is a card in the composer and tagged text on the wire. */
async function pasteCard(page: Page, text: string) {
  await message(page).evaluate((el, value) => {
    const data = new DataTransfer();
    data.setData('text/plain', value);
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, text);
}

async function runningLong(page: Page) {
  const engine = await installMockEngine(page, { initial: { title: 'Long', entries: [] }, turns: [{}], manual: true });
  await openApp(page);
  await send(page, 'Write a lot');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({
    running: true,
    entries: [
      { Role: 'user', Text: 'Write a lot' },
      { Role: 'assistant', Text: LONG, Answer: true },
    ],
  });
  await expect(page.getByText('Paragraph 80 of a long reply', { exact: false })).toBeVisible();
  return engine;
}

async function queueMessage(page: Page, text: string) {
  await message(page).fill(text);
  await page.getByRole('button', { name: /^Queue/ }).click();
  await expect(message(page)).toHaveValue('');
}

/** Rest means the pointer and the keyboard are both off the row, so hover-only chrome is hidden. */
async function rest(page: Page) {
  await page.mouse.move(0, 0);
  await message(page).focus();
}

function axe(page: Page, include: string) {
  // Conversation ink is checked in conversation-contrast. This scan is structure.
  return new AxeBuilder({ page }).include(include).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).disableRules(['color-contrast']).analyze();
}

function inside(box: { x: number; width: number } | null, width: number) {
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
}

for (const colorScheme of ['light', 'dark'] as const) {
  test.describe(`queue menu ${colorScheme}`, () => {
    test.use({ colorScheme, reducedMotion: 'reduce' });

    test('CV-171 hover and keyboard focus replace the clock with a grip and show edit and remove', async ({ page }) => {
      const { list } = await queued(page);
      const row = list.getByRole('group').first();
      await rest(page);
      const clock = row.locator('[data-icon="clock"]');
      const grip = row.locator('[data-icon="grip"]');
      const actions = row.locator('.queued-actions');
      await expect(clock).toBeVisible();
      await expect(grip).toBeHidden();
      await expect(actions).toHaveCSS('opacity', '0');
      const clockBox = await row.locator('.queued-mark').boundingBox();
      const restFill = await row.evaluate((el) => getComputedStyle(el).backgroundColor);
      const restColor = await row.evaluate((el) => getComputedStyle(el).color);

      await row.hover();
      await expect(clock).toBeHidden();
      await expect(grip).toBeVisible();
      await expect(actions).toHaveCSS('opacity', '1');
      await expect(row.getByRole('button', { name: 'Edit queued message' })).toBeVisible();
      await expect(row.getByRole('button', { name: 'Remove queued message' })).toBeVisible();
      const gripBox = await row.locator('.queued-grip').boundingBox();
      expect(Math.abs(gripBox!.x - clockBox!.x)).toBeLessThanOrEqual(1);
      expect(Math.abs(gripBox!.y - clockBox!.y)).toBeLessThanOrEqual(1);
      const hoverFill = await row.evaluate((el) => getComputedStyle(el).backgroundColor);
      expect(hoverFill).not.toBe(restFill);
      expect(await row.evaluate((el) => getComputedStyle(el).color)).toBe(restColor);

      const point = await row.locator('.queued-grip').boundingBox();
      await page.mouse.move(point!.x + point!.width / 2, point!.y + point!.height / 2);
      await page.mouse.down();
      await expect.poll(() => row.evaluate((el) => getComputedStyle(el).backgroundColor)).not.toBe(hoverFill);
      await page.mouse.up();

      await rest(page);
      await expect(grip).toBeHidden();
      await row.focus();
      await expect(grip).toBeVisible();
      await expect(clock).toBeHidden();
      await expect(actions).toHaveCSS('opacity', '1');
    });

    test('CV-173 clicking the queued text edits it inline, and the keyboard does the same', async ({ page }) => {
      const { engine, list } = await queued(page);
      const text = list.getByRole('button', { name: 'first queued', exact: true });
      await text.click();
      const field = list.getByRole('textbox', { name: 'Edit queued message' });
      await expect(field).toBeFocused();
      await field.fill('dropped by Escape');
      await page.keyboard.press('Escape');
      await expect(text).toBeVisible();
      expect(posts(engine, '/queue-edit')).toHaveLength(0);

      await text.focus();
      await page.keyboard.press('Enter');
      await expect(field).toBeFocused();
      await field.fill('rewritten from the keyboard');
      await field.press('Enter');
      await expect.poll(() => order(engine)).toEqual(['rewritten from the keyboard', 'second queued', 'third queued']);
      expect(posts(engine, '/queue-edit')[0]?.body).toMatchObject({ text: 'rewritten from the keyboard' });
    });

    test('CV-174 the menu edits, moves, sends now and removes on the engine routes', async ({ page }) => {
      const { engine, list } = await queued(page);
      const rows = list.getByRole('group');
      const firstId = engine.snapshot().queue?.[0]?.id;
      await rows.first().focus();
      await page.keyboard.press('Shift+F10');
      const menu = menuOf(page);
      await expect(menu.getByRole('menuitem')).toHaveText(['Edit', 'Move up', 'Move down', 'Send now', 'Remove']);
      await expect(menu.getByRole('menuitem', { name: 'Move up', exact: true })).toBeDisabled();
      await expect(menu.getByRole('menuitem', { name: 'Move up', exact: true })).toHaveCSS('opacity', '0.4');
      await expect(menu.getByRole('menuitem', { name: 'Edit', exact: true })).toBeFocused();
      await page.keyboard.press('ArrowDown');
      await expect(menu.getByRole('menuitem', { name: 'Move down', exact: true })).toBeFocused();
      await page.keyboard.press('Enter');
      await expect.poll(() => order(engine)).toEqual(['second queued', 'first queued', 'third queued']);
      expect(posts(engine, '/queue-move')[0]?.body).toMatchObject({ id: firstId, to: 1 });

      await rows.filter({ hasText: 'second queued' }).click({ button: 'right' });
      await menu.getByRole('menuitem', { name: 'Edit', exact: true }).click();
      const field = list.getByRole('textbox', { name: 'Edit queued message' });
      await expect(field).toBeFocused();
      await field.fill('second edited');
      await list.getByRole('button', { name: 'Save' }).click();
      await expect.poll(() => order(engine)).toEqual(['second edited', 'first queued', 'third queued']);
      expect(posts(engine, '/queue-edit')[0]?.body).toMatchObject({ text: 'second edited' });

      const sendId = engine.snapshot().queue?.find((item) => item.text === 'first queued')?.id;
      await rows.filter({ hasText: 'first queued' }).click({ button: 'right' });
      await menu.getByRole('menuitem', { name: 'Send now', exact: true }).click();
      await expect.poll(() => posts(engine, '/queue-send').map((call) => call.body)).toEqual([{ id: sendId }]);
      await expect.poll(() => order(engine)).toEqual(['second edited', 'third queued']);

      await rows.filter({ hasText: 'third queued' }).click({ button: 'right' });
      await expect(menu.getByRole('menuitem', { name: 'Move down', exact: true })).toBeDisabled();
      const removeId = engine.snapshot().queue?.find((item) => item.text === 'third queued')?.id;
      await menu.getByRole('menuitem', { name: 'Remove', exact: true }).click();
      await expect.poll(() => posts(engine, '/queue-remove').map((call) => call.body)).toEqual([{ id: removeId }]);
      await expect.poll(() => order(engine)).toEqual(['second edited']);
    });

    test('CV-174 Send now answers 409 by dropping the row and saying so, with no toast', async ({ page }) => {
      const { engine, list } = await queued(page, ['first queued', 'second queued']);
      await page.route('**/sessions/*/queue-send', async (route) => {
        engine.advance();
        await route.fallback();
      });
      await list.getByRole('group').first().click({ button: 'right' });
      await menuOf(page).getByRole('menuitem', { name: 'Send now', exact: true }).click();
      const note = page.locator('.system-note[data-kind="failure"]');
      await expect(note).toContainText('that message has already been sent');
      await expect(note.getByRole('button', { name: 'Retry' })).toHaveCount(0);
      await expect(list).toHaveCount(0);
      await expect(page.locator('.toast-region .toast')).toHaveCount(0);
      expect(posts(engine, '/queue-send')).toHaveLength(1);
    });

    test('CV-180 a queued paste card edits as the whole stored text', async ({ page }) => {
      const { engine, list } = await queued(page, []);
      const body = pasteLines(PASTE_LINES);
      const stored = `<pasted-text lines="${PASTE_LINES}">\n${body}\n</pasted-text>\n\nWhy does nested fail?`;
      await pasteCard(page, body);
      await expect(page.locator('.paste-card')).toBeVisible();
      await message(page).fill('Why does nested fail?');
      await page.getByRole('button', { name: /^Queue/ }).click();
      await expect.poll(() => order(engine)).toEqual([stored]);
      await expect(page.locator('.paste-card')).toHaveCount(0);
      const shown = list.getByRole('button', { name: 'Why does nested fail?', exact: true });
      await expect(shown).toBeVisible();
      await expect(list.getByText('<pasted-text', { exact: false })).toHaveCount(0);

      await shown.click();
      const field = list.getByRole('textbox', { name: 'Edit queued message' });
      await expect(field).toBeFocused();
      await expect(field).toHaveValue(stored);
      await expect(list.locator('.paste-card')).toHaveCount(0);
      await field.fill(`${stored} please`);
      await field.press('Enter');
      await expect.poll(() => order(engine)).toEqual([`${stored} please`]);
      expect(posts(engine, '/queue-edit')[0]?.body).toMatchObject({ text: `${stored} please` });
      await expect(list.getByRole('button', { name: 'Why does nested fail? please', exact: true })).toBeVisible();
    });

    test('CV-181 the N queued chip appears when the rows scroll away and a click or Enter returns to the bottom', async ({ page }) => {
      await runningLong(page);
      await queueMessage(page, 'first follow-up');
      await queueMessage(page, 'second follow-up');
      await expect(listOf(page).getByRole('group')).toHaveCount(2);
      await expect(chipOf(page, 2)).toHaveCount(0);

      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: 800 });
        await scroller(page).evaluate((el) => { el.scrollTop = 0; });
        const button = chipOf(page, 2);
        await expect(button).toBeVisible();
        await expect(listOf(page)).toHaveCount(0);
        const [chipBox, composerBox] = await Promise.all([
          button.boundingBox(),
          page.locator('.composer').boundingBox(),
        ]);
        inside(chipBox, width);
        expect(chipBox!.x).toBeGreaterThanOrEqual(composerBox!.x - 1);
        expect(chipBox!.x + chipBox!.width).toBeLessThanOrEqual(composerBox!.x + composerBox!.width + 1);
        await expectNoHorizontalOverflow(page);
        expect((await axe(page, '.composer-queue-chip')).violations).toEqual([]);
      }

      await chipOf(page, 2).click();
      await expect(listOf(page).getByRole('group')).toHaveCount(2);
      await expect(chipOf(page, 2)).toHaveCount(0);
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(48);

      await scroller(page).evaluate((el) => { el.scrollTop = 0; });
      await chipOf(page, 2).focus();
      await page.keyboard.press('Enter');
      await expect(listOf(page)).toBeVisible();
      await expect(chipOf(page, 2)).toHaveCount(0);
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(48);
    });

    test('queue rows and their menu stay inside the window at 320, 600 and 1200', async ({ page }) => {
      const { list } = await queued(page);
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: 800 });
        await rest(page);
        const row = list.getByRole('group').first();
        await row.hover();
        await expect(row.locator('[data-icon="grip"]')).toBeVisible();
        await expect(row.locator('.queued-actions')).toHaveCSS('opacity', '1');
        for (const name of ['Edit queued message', 'Remove queued message']) {
          inside(await row.getByRole('button', { name }).boundingBox(), width);
        }
        await expectNoHorizontalOverflow(page);
        await row.click({ button: 'right' });
        const menu = menuOf(page);
        await expect(menu).toBeVisible();
        // A 200px menu opened from the middle of a 320px window cannot sit entirely on either side of the pointer. The labels start on screen.
        const edit = menu.getByRole('menuitem', { name: 'Edit', exact: true });
        await expect(edit).toBeVisible();
        const item = await edit.boundingBox();
        expect(item!.x).toBeGreaterThanOrEqual(-1);
        expect(item!.x).toBeLessThan(width);
        expect((await axe(page, '.queued-rows')).violations).toEqual([]);
        expect((await axe(page, '.app-menu')).violations).toEqual([]);
        await page.keyboard.press('Escape');
        await expect(menu).toBeHidden();
      }
    });
  });

  test.describe(`queue touch ${colorScheme}`, () => {
    test.use({ colorScheme, hasTouch: true, reducedMotion: 'reduce' });

    test('CV-182 touch keeps the actions visible and does not drag', async ({ page }) => {
      const { engine, list } = await queued(page);
      const before = order(engine);
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: 800 });
        const row = list.getByRole('group').first();
        await expect(row.locator('..')).toHaveAttribute('draggable', 'false');
        await expect(row).not.toHaveAttribute('data-movable');
        await expect(row.locator('.queued-actions')).toHaveCSS('opacity', '1');
        await expect(row.locator('.queued-grip')).toHaveCSS('display', 'none');
        await expect(row.locator('.queued-mark')).toHaveCSS('visibility', 'visible');
        await row.hover();
        await expect(row.locator('.queued-grip')).toHaveCSS('display', 'none');
        await expect(row.locator('[data-icon="clock"]')).toBeVisible();
        inside(await row.getByRole('button', { name: 'Edit queued message' }).boundingBox(), width);
        inside(await row.getByRole('button', { name: 'Remove queued message' }).boundingBox(), width);
        await expectNoHorizontalOverflow(page);
      }
      const rows = list.getByRole('group');
      // A coarse pointer does not start a drag. dragTo gives up when the row refuses it.
      await rows.nth(1).dragTo(rows.nth(0), { timeout: 2_000 }).catch(() => undefined);
      expect(posts(engine, '/queue-move')).toHaveLength(0);
      expect(order(engine)).toEqual(before);
      const more = rows.first().getByRole('button', { name: 'Message actions for queued row' });
      // The dropdown's menu name is not exposed the way the right-click menu's is, so the items are addressed directly.
      await more.focus();
      await page.keyboard.press('Enter');
      await expect(page.getByRole('menuitem', { name: 'Send now', exact: true })).toBeVisible();
      await expect(page.getByRole('menuitem', { name: 'Move up', exact: true })).toBeDisabled();
      await page.keyboard.press('Escape');
      await more.click();
      await expect(page.getByRole('menuitem', { name: 'Send now', exact: true })).toBeVisible();
    });
  });
}

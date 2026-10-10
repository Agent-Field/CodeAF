import { expect, test, type Page } from '@playwright/test';
import { message, openApp, posts, send } from './support/conversation';
import { installMockEngine } from './support/mock-engine';

// Conversation 1b, CV-181: queued rows fold into the "N queued" chip once the
// reader is more than a viewport from the end, the same cutoff as the compact tray.
const LONG = Array.from({ length: 80 }, (_, at) => `Paragraph ${at + 1} of a long reply that fills the reading column.`).join('\n\n');

const scroller = (page: Page) => page.locator('.conversation-scroll');
const rows = (page: Page) => page.getByRole('list', { name: 'Queued messages' });
const chip = (page: Page, count: number) => page.getByRole('button', { name: `${count} queued`, exact: true });
const distance = (page: Page) => scroller(page).evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop);

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

for (const scheme of ['light', 'dark'] as const) {
  test.describe(`CV-181 ${scheme}`, () => {
    test.use({ colorScheme: scheme, reducedMotion: 'reduce' });

    test('the N queued chip replaces the rows past one viewport and a click brings them back', async ({ page }) => {
      await runningLong(page);
      await expect(chip(page, 1)).toHaveCount(0);
      await scroller(page).evaluate((el) => { el.scrollTop = 0; });
      await expect(chip(page, 1)).toHaveCount(0);

      await scroller(page).evaluate((el) => { el.scrollTop = el.scrollHeight; });
      await queueMessage(page, 'first follow-up');
      await queueMessage(page, 'second follow-up');
      await expect(rows(page).getByRole('listitem')).toHaveCount(2);
      await expect(chip(page, 2)).toHaveCount(0);

      // Exactly inside one viewport of the end the rows stay. The tray uses this same cutoff.
      await scroller(page).evaluate((el) => { el.scrollTop = Math.max(0, el.scrollHeight - el.clientHeight - window.innerHeight + 8); });
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(await page.evaluate(() => window.innerHeight));
      await expect(rows(page)).toBeVisible();
      await expect(chip(page, 2)).toHaveCount(0);

      await scroller(page).evaluate((el) => { el.scrollTop = Math.max(0, el.scrollHeight - el.clientHeight - window.innerHeight - 8); });
      const button = chip(page, 2);
      await expect(button).toBeVisible();
      await expect(rows(page)).toHaveCount(0);

      const model = page.locator('.model-picker');
      const measured = await button.evaluate((el) => {
        const style = getComputedStyle(el);
        const box = el.getBoundingClientRect();
        return {
          height: Math.round(box.height),
          fontSize: style.fontSize,
          fontWeight: style.fontWeight,
          padL: style.paddingLeft,
          padR: style.paddingRight,
          color: style.color,
          background: style.backgroundColor === 'transparent' ? 'rgba(0, 0, 0, 0)' : style.backgroundColor,
        };
      });
      expect(measured).toMatchObject({ height: 28, fontSize: '12px', fontWeight: '400', padL: '8px', padR: '8px', background: 'rgba(0, 0, 0, 0)' });
      expect(measured.color).toBe(await model.evaluate((el) => getComputedStyle(el).color));
      const order = await page.locator('.composer-tools button').evaluateAll((els) => els.map((el) => el.className));
      const modelAt = order.findIndex((name) => name.includes('model-picker'));
      const chipAt = order.findIndex((name) => name.includes('composer-queue-chip'));
      expect(chipAt).toBe(modelAt + 1);

      await button.hover();
      const hovered = await button.evaluate((el) => getComputedStyle(el).backgroundColor);
      expect(hovered).not.toBe('rgba(0, 0, 0, 0)');
      const box = await button.boundingBox();
      await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
      await page.mouse.down();
      const pressed = await button.evaluate((el) => getComputedStyle(el).backgroundColor);
      expect(pressed).not.toBe(hovered);
      // Releasing is the click: it re-anchors, so the chip leaves with the pointer still down.
      await page.mouse.up();
      await expect(rows(page).getByRole('listitem')).toHaveCount(2);
      await expect(chip(page, 2)).toHaveCount(0);
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(48);

      await scroller(page).evaluate((el) => { el.scrollTop = 0; });
      await expect(button).toBeVisible();
      await button.focus();
      await page.keyboard.press('Enter');
      await expect(rows(page)).toBeVisible();
      await expect(chip(page, 2)).toHaveCount(0);
      await expect.poll(() => distance(page)).toBeLessThanOrEqual(48);
    });
  });
}

test.describe('CV-181', () => {
  test.use({ reducedMotion: 'reduce' });

  test('one queued message reads 1 queued once the reader is more than a viewport away', async ({ page }) => {
    await runningLong(page);
    await queueMessage(page, 'only one');
    await expect(rows(page).getByText('only one')).toBeVisible();
    await expect(chip(page, 1)).toHaveCount(0);
    await scroller(page).evaluate((el) => { el.scrollTop = 0; });
    await expect(chip(page, 1)).toBeVisible();
    await expect(rows(page)).toHaveCount(0);
    await chip(page, 1).click();
    await expect(rows(page).getByText('only one')).toBeVisible();
    await expect(chip(page, 1)).toHaveCount(0);
    await expect.poll(() => distance(page)).toBeLessThanOrEqual(48);
  });

  test('at 320px the chip stays inside the composer', async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 800 });
    await runningLong(page);
    await queueMessage(page, 'first follow-up');
    await queueMessage(page, 'second follow-up');
    await scroller(page).evaluate((el) => { el.scrollTop = 0; });
    const button = chip(page, 2);
    await expect(button).toBeVisible();
    const [chipBox, composerBox] = await Promise.all([button.boundingBox(), page.locator('.composer').boundingBox()]);
    expect(chipBox!.x).toBeGreaterThanOrEqual(composerBox!.x - 1);
    expect(chipBox!.x + chipBox!.width).toBeLessThanOrEqual(composerBox!.x + composerBox!.width + 1);
    expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth)).toBe(true);
  });
});

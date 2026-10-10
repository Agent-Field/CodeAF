import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { streaming } from './support/scenarios';
import { message, openApp, posts, send } from './support/conversation';

// Three messages queued behind a running turn, through the real composer.
async function queued(page: Page, words = ['first queued', 'second queued', 'third queued']) {
  const engine = await installMockEngine(page, { ...streaming(), manual: true });
  await openApp(page);
  await send(page, 'Say hello');
  await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
  const list = page.getByRole('list', { name: 'Queued messages' });
  for (const [index, text] of words.entries()) {
    await message(page).fill(text);
    await page.getByRole('button', { name: /^Queue/ }).click();
    await expect.poll(() => engine.snapshot().queue?.length).toBe(index + 1);
  }
  return { engine, list };
}

const order = (engine: Awaited<ReturnType<typeof queued>>['engine']) => engine.snapshot().queue?.map(item => item.text);

test('queue order comes from the engine: two rows, then "N more queued"', async ({ page }) => {
  const { list } = await queued(page);
  await expect(list.getByRole('group')).toHaveCount(2);
  await page.getByRole('button', { name: '1 more queued' }).click();
  await expect(list.getByRole('group')).toHaveCount(3);
});

test('a queued row edits in the engine: Esc keeps it, Save sends the change', async ({ page }) => {
  const { engine, list } = await queued(page);
  const row = list.getByRole('group').first();
  await row.hover();
  await row.getByRole('button', { name: 'Edit queued message' }).click();
  const field = list.getByRole('textbox', { name: 'Edit queued message' });
  await expect(field).toBeFocused();
  await field.fill('dropped by Escape');
  await page.keyboard.press('Escape');
  await expect(list.getByText('first queued')).toBeVisible();
  expect(posts(engine, '/queue-edit')).toHaveLength(0);
  await row.focus();
  await row.hover();
  await row.getByRole('button', { name: 'Edit queued message' }).click();
  await field.fill('first, reworded');
  await list.getByRole('button', { name: 'Save' }).click();
  await expect(list.getByText('first, reworded')).toBeVisible();
  expect(posts(engine, '/queue-edit')[0].body).toMatchObject({ text: 'first, reworded' });
  expect(order(engine)).toEqual(['first, reworded', 'second queued', 'third queued']);
});

test('dragging a row to another place reorders the engine queue', async ({ page }) => {
  const { engine, list } = await queued(page);
  await page.getByRole('button', { name: '1 more queued' }).click();
  const rows = list.getByRole('group');
  await rows.nth(2).dragTo(rows.nth(0));
  await expect.poll(() => order(engine)).toEqual(['third queued', 'first queued', 'second queued']);
  await expect(rows.first()).toContainText('third queued');
  expect(posts(engine, '/queue-move')[0].body).toMatchObject({ to: 0 });
});

test('Alt+Up and Alt+Down move the focused row and keep focus on it', async ({ page }) => {
  const { engine, list } = await queued(page);
  const rows = list.getByRole('group');
  await rows.nth(1).focus();
  await page.keyboard.press('Alt+ArrowUp');
  await expect.poll(() => order(engine)).toEqual(['second queued', 'first queued', 'third queued']);
  await expect(rows.first()).toBeFocused();
  await expect(rows.first()).toContainText('second queued');
  await expect(page.getByRole('status').filter({ hasText: 'Moved to position 1 of 3' })).toHaveCount(1);
  await page.keyboard.press('Alt+ArrowDown');
  await expect.poll(() => order(engine)).toEqual(['first queued', 'second queued', 'third queued']);
  await expect(rows.nth(1)).toBeFocused();
  await expect(rows.nth(1)).toContainText('second queued');
  // Down past the fold opens it so the row stays on screen and focused.
  await page.keyboard.press('Alt+ArrowDown');
  await expect.poll(() => order(engine)).toEqual(['first queued', 'third queued', 'second queued']);
  await expect(list.getByRole('group').nth(2)).toBeFocused();
});

test('a message whose turn has started refuses the change, says so and drops the row', async ({ page }) => {
  const { engine, list } = await queued(page, ['first queued', 'second queued']);
  await expect(list.getByRole('group')).toHaveCount(2);
  const row = list.getByRole('group').first();
  await row.hover();
  await row.getByRole('button', { name: 'Edit queued message' }).click();
  await list.getByRole('textbox', { name: 'Edit queued message' }).fill('too late');
  // End the turn after Save leaves the browser, so the editor cannot disappear before the click.
  await page.route('**/sessions/*/queue-edit', async (route) => {
    engine.advance();
    await route.fallback();
  });
  await list.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByText('that message has already been sent')).toBeVisible();
  await expect(list).toHaveCount(0);
});

test('queued text opens the inline editor and Enter saves', async ({ page }) => {
  const { engine, list } = await queued(page);
  await list.getByRole('button', { name: 'first queued', exact: true }).click();
  const field = list.getByRole('textbox', { name: 'Edit queued message' });
  await expect(field).toBeFocused();
  await field.fill('edited by text click');
  await field.press('Enter');
  await expect.poll(() => order(engine)).toEqual(['edited by text click', 'second queued', 'third queued']);
});

test('queued context menu has bounded moves, edits, sends now and removes', async ({ page }) => {
  const { engine, list } = await queued(page);
  const rows = list.getByRole('group');
  await rows.first().click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Queued message actions' });
  await expect(menu.getByRole('menuitem')).toHaveText(['Edit', 'Move up', 'Move down', 'Send now', 'Remove']);
  await expect(menu.getByRole('menuitem', { name: 'Move up', exact: true })).toBeDisabled();
  await expect(menu.getByRole('menuitem', { name: 'Move up', exact: true })).toHaveCSS('opacity', '0.4');
  await menu.getByRole('menuitem', { name: 'Move down', exact: true }).click();
  await expect.poll(() => order(engine)).toEqual(['second queued', 'first queued', 'third queued']);
  await rows.first().focus();
  await page.keyboard.press('Shift+F10');
  await menu.getByRole('menuitem', { name: 'Edit', exact: true }).click();
  await expect(list.getByRole('textbox', { name: 'Edit queued message' })).toBeFocused();
  await page.keyboard.press('Escape');
  await rows.first().focus();
  await page.keyboard.press('Shift+F10');
  await menu.getByRole('menuitem', { name: 'Send now', exact: true }).click();
  await expect.poll(() => posts(engine, '/queue-send').length).toBe(1);
  await expect.poll(() => order(engine)).toEqual(['first queued', 'third queued']);
  await rows.nth(1).click({ button: 'right' });
  await expect(menu.getByRole('menuitem', { name: 'Move down', exact: true })).toBeDisabled();
  await menu.getByRole('menuitem', { name: 'Remove', exact: true }).click();
  await expect.poll(() => order(engine)).toEqual(['first queued']);
});

test.describe('coarse pointers', () => {
  test.use({ hasTouch: true });
  test('queue actions stay visible and dragging is off at narrow widths', async ({ page }) => {
    const { list } = await queued(page);
    await page.setViewportSize({ width: 320, height: 560 });
    const row = list.getByRole('group').first();
    await expect(row.locator('..')).toHaveAttribute('draggable', 'false');
    await expect(row.getByRole('button', { name: 'Edit queued message' })).toBeVisible();
    await expect(row.locator('.queued-actions')).toHaveCSS('opacity', '1');
    await row.getByRole('button', { name: 'Message actions for queued row' }).click();
    await expect(page.getByRole('menuitem', { name: 'Send now', exact: true })).toBeVisible();
  });
});

for (const colorScheme of ['light', 'dark'] as const) {
  test.describe(`queue geometry ${colorScheme}`, () => {
    test.use({ colorScheme, reducedMotion: 'reduce' });
    test('rows preserve measured design geometry and hover changes only fill', async ({ page }) => {
      const { list } = await queued(page);
      const row = list.getByRole('group').first();
      await page.mouse.move(0, 0);
      const measure = () => row.evaluate(el => {
        const css = getComputedStyle(el);
        return { height: el.getBoundingClientRect().height, font: css.fontSize, radius: css.borderRadius, color: css.color, pad: css.padding, fill: css.backgroundColor };
      });
      const rest = await measure();
      expect(rest).toMatchObject({ height: 30, font: '12px', radius: '10px' });
      await row.hover();
      const hover = await measure();
      expect(hover.color).toBe(rest.color);
      expect(hover.pad).toBe(rest.pad);
      expect(hover.fill).not.toBe(rest.fill);
      await row.click({ button: 'right' });
      await expect(page.getByRole('menuitem', { name: 'Move up', exact: true })).toHaveCSS('opacity', '0.4');
    });
  });
}

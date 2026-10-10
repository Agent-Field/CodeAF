import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { installMockEngine } from './support/mock-engine';
import { openApp, send } from './support/conversation';
import type { EngineQuestion } from '../../src/features/chat/engine-client';

const question: EngineQuestion = { id: 1, kind: 'choice', ask: 'choice', head: 'Choose storage', options: [{ key: 'one', label: 'Local' }] };
const running = { ID: 'task-1', Title: 'Build it', Status: 'running', Waits: [], Seat: 'worker', Steps: 1 };
const done = { ...running, Status: 'done' };

/** Opens a conversation titled by the engine; `tasks` decides whether anything is running. Returns the engine for later updates. */
async function open(page: Page, tasks: object[] = [running]) {
  const engine = await installMockEngine(page, {
    initial: { entries: [] },
    turns: [
      { entries: [{ Role: 'assistant', Text: 'Ready.', Answer: true }], patch: { title: 'Header chat', tasks } },
      { entries: [{ Role: 'assistant', Text: 'Again.', Answer: true }], patch: { title: 'Engine later', tasks } },
    ],
  });
  await openApp(page);
  await send(page, 'Start');
  return engine;
}

const bar = (page: Page) => page.locator('.conversation-bar');
const title = (page: Page) => bar(page).locator('.conversation-bar-title');
const counts = (page: Page) => page.locator('.conversation-bar-counts');

for (const theme of ['light', 'dark'] as const) {
  test.describe(`conversation header · ${theme}`, () => {
    test.beforeEach(async ({ page }) => { await page.emulateMedia({ colorScheme: theme }); });

    test('shows the title, and the dot breathes only while something runs (CV-001, CV-002)', async ({ page }) => {
      await open(page);
      await expect(title(page)).toHaveText('Header chat');
      await expect(counts(page).locator('.cf-breathe')).toHaveCSS('animation-name', 'cf-breathe');
      await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
      await expect(counts(page).locator('.cf-breathe')).toHaveCSS('animation-name', 'none');
    });

    test('with nothing running there is no dot and no running count', async ({ page }) => {
      await open(page, [done]);
      await expect(title(page)).toHaveText('Header chat');
      await expect(counts(page).locator('.cf-breathe')).toHaveCount(0);
      await expect(counts(page).getByRole('button', { name: /running/ })).toHaveCount(0);
    });

    test('double-click renames, the tab follows, and a later engine title does not win (CV-006, CV-007)', async ({ page }) => {
      await open(page);
      await title(page).dblclick();
      const field = bar(page).getByRole('textbox', { name: 'Rename conversation' });
      await field.fill('My name');
      await field.press('Enter');
      await expect(title(page)).toHaveText('My name');
      await expect(page.getByRole('tab', { name: /My name/ })).toBeVisible();
      await send(page, 'More');
      await expect(page.getByText('Again.')).toBeVisible();
      await expect(title(page)).toHaveText('My name');
      await expect(page.getByRole('tab', { name: /My name/ })).toBeVisible();
    });

    test('the keyboard renames: Enter starts, Esc cancels, Enter commits (CV-008)', async ({ page }) => {
      await open(page);
      await title(page).focus();
      await page.keyboard.press('Enter');
      const field = bar(page).getByRole('textbox', { name: 'Rename conversation' });
      await field.fill('Discarded');
      await page.keyboard.press('Escape');
      await expect(title(page)).toHaveText('Header chat');
      await title(page).focus();
      await page.keyboard.press('Enter');
      await bar(page).getByRole('textbox', { name: 'Rename conversation' }).fill('Kept');
      await page.keyboard.press('Enter');
      await expect(title(page)).toHaveText('Kept');
      await expect(page.getByRole('tab', { name: /Kept/ })).toBeVisible();
    });

    test('count buttons show the keyboard ring and no ring on a mouse click (CV-012)', async ({ page }) => {
      const engine = await open(page);
      engine.update({ questions: [question] });
      const run = counts(page).getByRole('button', { name: /running/ });
      await expect(run).toBeVisible();
      await page.keyboard.press('Tab');
      await run.focus();
      await page.keyboard.press('Shift+Tab');
      await page.keyboard.press('Tab');
      await expect(run).toBeFocused();
      const ring = await run.evaluate(el => getComputedStyle(el).outlineStyle + '|' + getComputedStyle(el).outlineWidth + '|' + getComputedStyle(el).boxShadow);
      expect(ring).not.toMatch(/^none\|[^|]*\|none$/);
    });

    for (const width of [320, 600, 1200]) {
      test(`counts at ${width}px (CV-010)`, async ({ page }) => {
        await page.setViewportSize({ width, height: 800 });
        const engine = await open(page);
        engine.update({ questions: [question] });
        const bars = bar(page).first();
        await expect(title(page)).toBeVisible();
        const wrapped = (await bars.evaluate(el => getComputedStyle(el).flexWrap)) === 'wrap';
        expect(wrapped).toBe(width <= 600);
        // Counts stay reachable, never clipped past the pane edge.
        const box = await counts(page).first().boundingBox();
        if (box && await counts(page).first().isVisible()) expect(box.x + box.width).toBeLessThanOrEqual(width + 1);
        expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth)).toBe(true);
      });
    }

    test('the header adds no axe violations', async ({ page }) => {
      await open(page);
      const result = await new AxeBuilder({ page }).include('.conversation-bar').analyze();
      expect(result.violations).toEqual([]);
    });
  });
}

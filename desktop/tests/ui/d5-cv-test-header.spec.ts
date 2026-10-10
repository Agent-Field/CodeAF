import { test, expect } from '@playwright/test';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { openApp, send } from './support/conversation';
import type { EngineQuestion } from '../../src/features/chat/engine-client';

const question: EngineQuestion = { id: 1, kind: 'choice', ask: 'choice', head: 'Choose storage', options: [{ key: 'one', label: 'Local' }] };

/** A conversation with one running task and one question, so both header counts and the panel exist. */
async function openWithTasks(page: import('@playwright/test').Page): Promise<MockEngine> {
  const engine = await installMockEngine(page, {
    initial: { entries: [] },
    turns: [{ entries: [{ Role: 'assistant', Text: 'Ready.', Answer: true }], patch: {
      title: 'Header chat',
      tasks: [{ ID: 'task-1', Title: 'Build it', Status: 'running', Waits: [], Seat: 'worker', Steps: 1 }],
    } }],
  });
  await openApp(page);
  await send(page, 'Start');
  engine.update({ questions: [question] });
  await expect(page.locator('.conversation-bar-counts').getByRole('button', { name: /need/ })).toBeVisible();
  return engine;
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(`conversation header wiring · ${theme}`, () => {
    test.beforeEach(async ({ page }) => { await page.emulateMedia({ colorScheme: theme }); });

    test('the counts open the expanded Tasks view on their filter (I-ICV-33/34)', async ({ page }) => {
      await openWithTasks(page);
      await page.locator('.conversation-bar-counts').getByRole('button', { name: /running/ }).click();
      await expect(page.locator('.tasks-table-tab[data-filter="running"]')).toHaveAttribute('aria-pressed', 'true');
      await page.getByRole('button', { name: 'Back to the conversation' }).click();
      await page.locator('.conversation-bar-counts').getByRole('button', { name: /need/ }).click();
      await expect(page.locator('.tasks-table-tab[data-filter="needs"]')).toHaveAttribute('aria-pressed', 'true');
    });

    test('⌘⇧K toggles the task panel like the header button (I-IKY-16)', async ({ page }) => {
      await openWithTasks(page);
      const panel = page.getByRole('complementary', { name: 'Tasks' });
      const before = await panel.count();
      await page.keyboard.press('ControlOrMeta+Shift+K');
      await expect(panel).toHaveCount(before ? 0 : 1);
      await page.keyboard.press('ControlOrMeta+Shift+K');
      await expect(panel).toHaveCount(before);
    });
  });
}

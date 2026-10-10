import { test, expect, type Locator, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';
import { message, openApp, posts, send } from './support/conversation';

const tray = (page: Page) => page.getByRole('region', { name: 'Waiting on you' });

/** Send once so the tab owns a session, then the engine asks; a reload reads the new state at once. */
async function openWithQuestions(page: Page, questions: EngineQuestion[]): Promise<MockEngine> {
  const base = pendingQuestion();
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
  await openApp(page);
  await send(page, 'Set up storage');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({ needsPerson: true, running: true, questions });
  await expect(async () => {
    await page.reload();
    await expect(tray(page)).toBeVisible({ timeout: 1500 });
  }).toPass({ timeout: 15_000 });
  return engine;
}

const opacity = (locator: Locator) => locator.evaluate((el) => Number(getComputedStyle(el).opacity));

const asked = (second: number) => `2026-10-09T10:00:0${second}Z`;

/** Three pages: a radio card, an empty text field, then a last choice. Oldest first. */
function pagerQuestions(): EngineQuestion[] {
  return [
    {
      id: 1, kind: 'ask', ask: 'choice', head: 'Pick a database', asked: asked(1),
      options: [
        { key: 'sqlite', label: 'SQLite', body: 'One file.' },
        { key: 'postgres', label: 'Postgres', body: 'A server.' },
      ],
    },
    { id: 2, kind: 'ask', ask: 'clarification', head: 'Name the cache', asked: asked(2), input: { kind: 'text', prompt: 'Your answer' } },
    {
      id: 3, kind: 'ask', ask: 'choice', head: 'Pick a region', asked: asked(3),
      options: [{ key: 'e', label: 'East' }, { key: 'w', label: 'West' }],
    },
  ];
}

test('pager-keys: ArrowLeft and ArrowRight page while the tray is focused', async ({ page }) => {
  await openWithQuestions(page, pagerQuestions());
  const card = tray(page);
  const count = card.locator('.tray-count');
  const previous = card.getByRole('button', { name: 'Previous question' });
  const next = card.getByRole('button', { name: 'Next question' });

  await expect(count).toHaveText('1 of 3');
  await expect(count).toHaveAccessibleName('Question 1 of 3');
  await expect(previous).toBeDisabled();
  expect(await opacity(previous)).toBeCloseTo(0.4, 2);
  expect(await opacity(next)).toBeCloseTo(1, 2);

  // The message box is outside the card, so its arrows do not page.
  await message(page).focus();
  await page.keyboard.press('ArrowRight');
  await expect(count).toHaveAccessibleName('Question 1 of 3');

  // A radio keeps the arrows: they move the choice, they do not page.
  await card.getByRole('radio', { name: /SQLite/ }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(count).toHaveAccessibleName('Question 1 of 3');
  await expect(card.getByRole('radio', { name: /Postgres/ })).toBeChecked();

  await next.focus();
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('Control+ArrowRight');
  await expect(count).toHaveAccessibleName('Question 1 of 3');

  await page.keyboard.press('ArrowRight');
  await expect(count).toHaveAccessibleName('Question 2 of 3');
  const answer = card.getByRole('textbox', { name: 'Your answer' });
  await expect(answer).toBeFocused();

  // An empty field pages. At the last question the next arrow is disabled and a further press does nothing.
  await page.keyboard.press('ArrowRight');
  await expect(count).toHaveAccessibleName('Question 3 of 3');
  await expect(card.getByRole('region', { name: 'Pick a region' })).toBeVisible();
  await expect(next).toBeDisabled();
  expect(await opacity(next)).toBeCloseTo(0.4, 2);
  await page.keyboard.press('ArrowRight');
  await expect(count).toHaveAccessibleName('Question 3 of 3');

  await previous.focus();
  await page.keyboard.press('ArrowLeft');
  await expect(count).toHaveAccessibleName('Question 2 of 3');
  await expect(answer).toBeFocused();

  // Any character, including a space, keeps the arrows for the caret.
  await answer.fill(' ');
  await answer.press('ArrowRight');
  await answer.press('ArrowLeft');
  await expect(count).toHaveAccessibleName('Question 2 of 3');
  await expect(answer).toHaveValue(' ');

  await answer.fill('redis');
  await answer.press('ArrowRight');
  await answer.press('ArrowLeft');
  await expect(count).toHaveAccessibleName('Question 2 of 3');
  await expect(answer).toHaveValue('redis');

  await answer.fill('');
  await answer.press('ArrowLeft');
  await expect(count).toHaveAccessibleName('Question 1 of 3');
  await expect(previous).toBeDisabled();
  await page.keyboard.press('ArrowLeft');
  await expect(count).toHaveAccessibleName('Question 1 of 3');
});

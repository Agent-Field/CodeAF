import { test, expect, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { pendingQuestion, plainReply } from './support/scenarios';
import { timedProposal, trayQuestions } from './support/scenarios-v2';
import { expectAccessible } from './contracts';
import { expectNoHorizontalOverflow, message, openApp, posts, send } from './support/conversation';

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

test('a question sits in the tray; choosing answers canonically and leaves a receipt in the flow', async ({ page }) => {
  const engine = await openWithQuestions(page, pendingQuestion().initial.questions!);
  const card = tray(page).getByRole('region', { name: 'Pick a database' });
  await expect(card).toBeVisible();
  await expect(page.getByRole('button', { name: 'Waiting on you: Pick a database' })).toBeVisible();
  // Not blocking the turn: the composer stays open.
  await expect(message(page)).toBeEnabled();
  // 1a: a question that does not hold the reply says so at the right end of the card's action row, not under the card.
  const note = card.getByText('Doesn’t block this reply');
  await expect(note).toBeVisible();
  const [noteBox, choose, cardBox] = await Promise.all([note.boundingBox(), card.getByRole('button', { name: 'Choose' }).boundingBox(), card.boundingBox()]);
  expect(Math.abs(noteBox!.y + noteBox!.height / 2 - (choose!.y + choose!.height / 2))).toBeLessThan(choose!.height / 2);
  expect(noteBox!.x).toBeGreaterThan(choose!.x + choose!.width);
  expect(cardBox!.x + cardBox!.width - (noteBox!.x + noteBox!.width)).toBeLessThan(40);
  await card.getByRole('radio', { name: /Postgres/ }).check();
  await card.getByRole('button', { name: 'Choose' }).click();
  await expect.poll(() => posts(engine, '/answer').length).toBe(1);
  expect(posts(engine, '/answer')[0].body).toMatchObject({ kind: 'choice', id: 7, key: 'postgres', picked: ['postgres'] });
  await expect(tray(page)).toHaveCount(0);
  await expect(page.getByText(/^Postgres you · \d\d:\d\d$/)).toBeVisible();
});

test('several questions page through one card; a permission set is one card; the composer blocks only on a blocking question', async ({ page }) => {
  const engine = await openWithQuestions(page, trayQuestions());
  const count = tray(page).getByRole('status');
  await expect(count).toHaveText(/^1 of 2$/);
  await expect(message(page)).toBeDisabled();
  await page.getByRole('button', { name: 'Waiting on you: Pick a database' }).click();
  await expect(tray(page).getByRole('region', { name: 'Pick a database' })).toBeVisible();
  await tray(page).getByRole('button', { name: 'Next question' }).click();
  await expect(count).toHaveText(/^2 of 2$/);
  await tray(page).getByRole('button', { name: 'Previous question' }).click();
  await tray(page).getByRole('button', { name: 'Next question' }).click();
  const set = tray(page).getByRole('region', { name: 'Allow 3 actions?' });
  await set.getByRole('button', { name: 'Allow all' }).click();
  await expect.poll(() => posts(engine, '/answer').length).toBe(3);
  expect(posts(engine, '/answer').map((call) => [call.body.id, call.body.key])).toEqual([[1, '1'], [2, '1'], [3, '1']]);
  await expect(count).toHaveCount(0);
  await expect(tray(page).getByRole('region', { name: 'Pick a database' })).toBeVisible();
  await expect(message(page)).toBeEnabled();
});

test('a question with a deadline counts down and Hold stops its clock', async ({ page }) => {
  const engine = await openWithQuestions(page, [timedProposal()]);
  const clock = tray(page).getByRole('timer');
  await expect(clock).toHaveText(/^Starts in \dm \d\ds$/);
  await tray(page).getByRole('button', { name: 'Hold' }).click();
  await expect.poll(() => posts(engine, '/questions/hold').length).toBe(1);
  expect(posts(engine, '/questions/hold')[0].body).toMatchObject({ kind: 'task', id: 8 });
  await expect(clock).toHaveText('On hold — take your time');
});

for (const scheme of ['light', 'dark'] as const) {
  test(`320px ${scheme}: no horizontal overflow, composer reachable, accessible`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await page.setViewportSize({ width: 320, height: 568 });
    await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
    await openApp(page);
    await expectNoHorizontalOverflow(page);
    await expectAccessible(page);
    await send(page, 'Explain the add helper');
    await expect(page.getByRole('table')).toBeVisible();
    await expectNoHorizontalOverflow(page);
    await expect(message(page)).toBeInViewport();
    await message(page).fill('another');
    await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeInViewport();
    await expectAccessible(page);
  });
}

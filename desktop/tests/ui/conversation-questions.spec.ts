import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion, plainReply } from './support/scenarios';
import { expectAccessible } from './contracts';
import { expectNoHorizontalOverflow, message, openApp, posts, send } from './support/conversation';

/** Send once so the tab owns a session, then the pending question is on screen. */
async function openWithQuestion(page: Page) {
  const base = pendingQuestion();
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
  await openApp(page);
  await send(page, 'Set up storage');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({ needsPerson: true, running: true, questions: base.initial.questions });
  return engine;
}

test('a pending question is a card; choosing an option answers canonically and clears the card', async ({ page }) => {
  const engine = await openWithQuestion(page);
  const card = page.getByRole('region', { name: 'Pick a database' });
  await expect(async () => {
    await page.reload();
    await expect(card).toBeVisible({ timeout: 1500 });
  }).toPass({ timeout: 15_000 });
  await expect(card.getByText('Which database should the app use?')).toBeVisible();
  await card.getByRole('button', { name: 'Postgres' }).click();
  await expect.poll(() => posts(engine, '/answer').length).toBe(1);
  expect(posts(engine, '/answer')[0].body).toMatchObject({ kind: 'choice', id: 7, key: 'postgres', picked: ['postgres'] });
  await expect(card).toHaveCount(0);
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

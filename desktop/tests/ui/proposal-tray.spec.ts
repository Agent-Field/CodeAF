import { test, expect, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';
import { openApp, posts, send } from './support/conversation';

/** Marketing is still learning this kind of question: it proposes "Keep strict" at 92% and asks for one click. */
const proposal = (): EngineQuestion => ({
  id: 31,
  kind: 'ask',
  ask: 'choice',
  head: 'Should trailing commas be on by default?',
  proposal: { place: 'Marketing', agreed: 14, of: 20 },
  pick: { key: 'a', percent: 92, basis: ['k-12', 'k-40'] },
  options: [{ key: 'a', label: 'Keep strict' }, { key: 'b', label: 'Tolerant' }],
});

async function open(page: Page) {
  const base = pendingQuestion();
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
  await openApp(page);
  await send(page, 'Set up storage');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({ needsPerson: true, running: true, questions: [proposal()] });
  const card = page.getByRole('region', { name: 'Should trailing commas be on by default?' });
  await expect(async () => {
    await page.reload();
    await expect(card).toBeVisible({ timeout: 1500 });
  }).toPass({ timeout: 15_000 });
  return { engine, card };
}

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.use({ colorScheme: scheme });

    test('I2.8: the learning count heads the tray and the proposed choice is lit with what it knows', async ({ page }) => {
      const { card } = await open(page);
      const tray = page.getByRole('region', { name: 'Waiting on you' });
      await expect(tray.locator('.tray-crumb')).toHaveText('Marketing is learning');
      await expect(tray.locator('.tray-crumb-kind')).toHaveText('· 14 of 20 agreed');
      const choices = card.locator('.proposal-choice');
      await expect(choices).toHaveCount(2);
      await expect(choices.first().locator('.proposal-knows')).toHaveText('Would choose · 92% · matches what Marketing knows');
      await expect(choices.first()).toHaveAttribute('title', 'Relied on k-12, k-40');
      const grid = await card.locator('.proposal-choices').evaluate((el) => { const s = getComputedStyle(el); return { columns: s.gridTemplateColumns.split(' ').length, gap: s.columnGap }; });
      expect(grid).toEqual({ columns: 2, gap: '6px' });
      const fill = await choices.first().evaluate((el) => getComputedStyle(el).backgroundColor);
      const other = await choices.nth(1).evaluate((el) => getComputedStyle(el).backgroundColor);
      expect(fill).not.toBe(other);
      await expect(card.getByRole('button', { name: /^Agree/ })).toBeVisible();
      await expect(card.getByRole('button', { name: 'Choose Tolerant' })).toBeVisible();
    });

    test('Agree sends the proposed key without an overrule', async ({ page }) => {
      const { engine, card } = await open(page);
      await card.getByRole('button', { name: /^Agree/ }).click();
      await expect.poll(() => posts(engine, '/answer').length).toBe(1);
      const body = posts(engine, '/answer')[0].body as Record<string, unknown>;
      expect(body).toMatchObject({ id: 31, key: 'a', picked: ['a'] });
      expect(body.overruled).toBeUndefined();
    });

    test('Choose sends the other key and records it as overruled', async ({ page }) => {
      const { engine, card } = await open(page);
      await card.getByRole('button', { name: 'Choose Tolerant' }).click();
      await expect.poll(() => posts(engine, '/answer').length).toBe(1);
      expect(posts(engine, '/answer')[0].body).toMatchObject({ id: 31, key: 'b', picked: ['b'], overruled: true });
    });
  });
}

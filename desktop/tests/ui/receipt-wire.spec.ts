import { test, expect } from '@playwright/test';
import type { EngineEntry } from '../../src/features/chat/engine-client';
import { installMockEngine } from './support/mock-engine';
import { openApp, send } from './support/conversation';

const why = { by: 'Config parser', because: 'you always allow this here', percent: 94, reversible: true };
const plan = { id: 'p-1', reachesBeyond: true, steps: [{ kind: 'hold' as const, target: { chat: 'launch' }, text: 'Hold Launch post' }] };

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.use({ colorScheme: scheme });
    test('canonical receipts and grouped receipts redraw after reload', async ({ page }) => {
      await installMockEngine(page, { turns: [{ entries: [
        { Role: 'aside', Text: 'Allowed automatically', AsideKind: 'decision-receipt', Decision: { kind: 'decision-receipt', text: 'Allowed automatically by Config parser · you always allow this here · Why?', why } },
        { Role: 'aside', Text: 'Did 2 things', AsideKind: 'decision-receipt', Decision: { kind: 'decision-receipt', text: 'Did 2 things', children: [
          { decisionId: 'd1', text: 'held Launch post', question: { kind: 'ask', token: '1' }, why },
          { decisionId: 'd2', text: 'saved a note', question: { kind: 'ask', token: '2' }, why },
        ] } },
        { Role: 'aside', Text: 'Remembered: run the suite', AsideKind: 'remember-line' },
      ], patch: { running: false } }], initial: {} });
      await openApp(page);
      await send(page, 'Coordinate launch');
      const single = page.locator('.decision-receipt').first();
      await expect(single).toContainText('Allowed automatically by Config parser');
      expect(await single.evaluate(el => getComputedStyle(el).fontSize)).toBe('12px');
      await page.getByRole('button', { name: /Did 2 things/ }).click();
      await expect(page.locator('.decision-receipt')).toHaveCount(3);
      await single.getByRole('button', { name: 'Why?' }).click();
      await expect(page.getByRole('note').filter({ hasText: '94%' })).toBeVisible();
      await expect(page.getByText('Remembered: run the suite')).toBeVisible();
      await page.reload();
      await expect(page.locator('.decision-receipt')).toHaveCount(1);
      await expect(page.getByRole('button', { name: /Did 2 things/ })).toBeVisible();
    });
    test('canonical proposal keeps the 40px compact Review line', async ({ page }) => {
      const history: EngineEntry[] = Array.from({ length: 30 }, (_, i) => [
        { Role: 'user' as const, Text: `Earlier request ${i}` },
        { Role: 'assistant' as const, Text: 'Recorded response. '.repeat(400), Answer: true },
      ]).flat();
      await installMockEngine(page, { initial: {}, turns: [{ entries: history, patch: { running: true, needsPerson: true, questions: [{ id: 31, kind: 'ask', ask: 'choice', head: 'Choose storage', proposal: { place: 'Software', agreed: 14, of: 20 }, pick: { key: 'a' }, options: [{ key: 'a', label: 'SQLite' }, { key: 'b', label: 'Postgres' }] }] } }] });
      await openApp(page);
      await send(page, 'Choose storage');
      await expect(page.locator('.proposal')).toBeVisible();
      await expect.poll(() => page.locator('.conversation-scroll').evaluate(el => el.scrollHeight - el.clientHeight)).toBeGreaterThan(800);
      await page.locator('.conversation-scroll').evaluate(el => { el.scrollTop = 0; el.dispatchEvent(new Event('scroll')); });
      const compact = page.getByRole('region', { name: 'Questions waiting, summary' });
      await expect(compact).toBeVisible();
      expect(await compact.evaluate(el => el.getBoundingClientRect().height)).toBe(40);
      await compact.getByRole('button', { name: 'Review' }).click();
      await expect(page.locator('.proposal')).toBeVisible();
    });
    for (const verb of ['go', 'cancel'] as const) {
      test(`plan ${verb} uses its canonical route`, async ({ page }) => {
        let writes = 0;
        await installMockEngine(page, { initial: {}, turns: [{ entries: [{ Role: 'aside', Text: 'Plan', AsideKind: 'plan', Plan: plan }], patch: { running: false } }] });
        await page.route(`**/sessions/*/plan/p-1/${verb}`, async route => {
          expect(route.request().method()).toBe('POST');
          writes++;
          await route.fulfill({ json: verb === 'go' ? { planId: 'p-1', results: [{ step: plan.steps[0], status: 'done', line: 'Held Launch post' }] } : { cancelled: true } });
        });
        await openApp(page);
        await send(page, 'Hold launch');
        const card = page.getByRole('region', { name: 'Plan', exact: true });
        await expect(card).toBeVisible();
        await card.getByRole('button', { name: verb === 'go' ? 'Go' : 'Cancel', exact: true }).click();
        await expect(card).toHaveCount(0);
        expect(writes).toBe(1);
        if (verb === 'go') await expect(page.getByText('Held Launch post', { exact: true })).toBeVisible();
      });
    }
  });
}

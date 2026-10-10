import { test, expect, type Page } from '@playwright/test';
import type { EngineEntry, EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';
import { openApp, posts, send } from './support/conversation';
import { WORDS, why } from './fixtures/decisions';

// Decisions 12a to 12d and the Components I2 specimens, driven through the real app over the mock engine. Component-level geometry has its own
// suites (src/features/decisions/*.test.tsx); this one asserts the exact sentences and the wires between the pieces.

const planStep = (kind: 'hold' | 'ask-place' | 'remember', chat: string, text: string) => ({ kind, target: kind === 'hold' ? { chat } : kind === 'ask-place' ? { place: chat } : {}, text });
const plan = { id: 'p-1', reachesBeyond: true, steps: [
  planStep('hold', 'launch', 'Hold Launch post until the v1 suite passes'),
  planStep('ask-place', 'software', 'Ask Software to run the v1 suite today'),
  planStep('remember', '', 'Remember: run the v1 suite before any launch'),
] };
const receipts: EngineEntry[] = [
  { Role: 'aside', Text: 'Allowed automatically', AsideKind: 'decision-receipt', Decision: { kind: 'decision-receipt', text: WORDS.receipt, why } },
  { Role: 'aside', Text: 'Did 2 things', AsideKind: 'decision-receipt', Decision: { kind: 'decision-receipt', text: 'Did 2 things', children: [
    { decisionId: 'd1', text: 'held Launch post', question: { kind: 'ask', token: '1' }, why },
    { decisionId: 'd2', text: 'saved a note', question: { kind: 'ask', token: '2' }, why },
  ] } },
];
const proposal = (): EngineQuestion => ({
  id: 31, kind: 'ask', ask: 'choice', head: 'Which pricing line should lead?',
  proposal: { place: 'Marketing', agreed: 14, of: 20 }, pick: { key: 'a', percent: 92, basis: ['k-12'] },
  options: [{ key: 'a', label: 'Free for one seat, $12 per teammate' }, { key: 'b', label: 'Usage-based, from $0' }],
});

async function openDetail(page: Page, dependents = 2) {
  await page.goto('/');
  await page.evaluate(async ({ dependents }) => {
    const path = '/tests/ui/fixtures/decisions-mount.ts';
    const { mountDecisionDetail } = await import(path);
    mountDecisionDetail(localStorage.getItem('codeaf-theme') ?? 'light', dependents);
  }, { dependents });
}

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.use({ colorScheme: scheme });
    test.beforeEach(async ({ page }) => { await page.addInitScript(t => localStorage.setItem('codeaf-theme', t), scheme); });

    test('12b: receipts are one 12px quiet line, grouped receipts say "Did 2 things", and Why? names the place and the reason', async ({ page }) => {
      await installMockEngine(page, { initial: {}, turns: [{ entries: receipts, patch: { running: false } }] });
      await openApp(page);
      await send(page, 'Coordinate launch');
      const lines = page.locator('.decision-receipt');
      await expect(lines.first()).toHaveText(WORDS.receipt);
      expect(await lines.first().evaluate(el => getComputedStyle(el).fontSize)).toBe('12px');
      const group = page.getByRole('button', { name: /Did 2 things/ });
      await expect(group).toHaveText(WORDS.group);
      await expect(group).toHaveAttribute('aria-expanded', 'false');
      await group.click();
      await expect(group).toHaveAttribute('aria-expanded', 'true');
      await expect(lines).toHaveCount(3);
      await lines.first().getByRole('button', { name: 'Why?' }).click();
      await expect(page.getByRole('note').filter({ hasText: 'Config parser · you always allow this here · 94%' })).toBeVisible();
    });

    test('12b: the plan card shows its head, three steps and Go · Edit · Cancel, and Go posts to the plan route', async ({ page }) => {
      await installMockEngine(page, { initial: {}, turns: [{ entries: [{ Role: 'aside', Text: 'Plan', AsideKind: 'plan', Plan: plan }], patch: { running: false } }] });
      const sent: string[] = [];
      await page.route('**/sessions/*/plan/p-1/go', route => { sent.push(route.request().method()); return route.fulfill({ json: { planId: 'p-1', results: plan.steps.map(step => ({ step, status: 'done', line: `Done: ${step.text}` })) } }); });
      await openApp(page);
      await send(page, 'Hold launch');
      const card = page.getByRole('region', { name: 'Plan', exact: true });
      await expect(card.locator('.plan-card-head')).toHaveText(WORDS.planHead);
      await expect(card.locator('.plan-card-step')).toHaveText(plan.steps.map(step => step.text));
      await expect(card.getByRole('button')).toHaveText(['Go', 'Edit', 'Cancel']);
      const box = await card.boundingBox();
      const rows = await card.locator('.plan-card-step').evaluateAll(els => els.map(el => el.getBoundingClientRect().height));
      expect(box!.width).toBeGreaterThan(200);
      expect(new Set(rows).size).toBe(1);
      expect(await card.evaluate(el => parseFloat(getComputedStyle(el).borderTopLeftRadius))).toBeGreaterThan(0);
      await card.getByRole('button', { name: 'Go', exact: true }).click();
      await expect(card).toHaveCount(0);
      expect(sent).toEqual(['POST']);
      await expect(page.getByText(`Done: ${plan.steps[0].text}`)).toBeVisible();
    });

    test('12c: the proposal tray heads with the learning count, lights the proposed choice and sits on a 2-column grid', async ({ page }) => {
      const base = pendingQuestion();
      const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
      await openApp(page);
      await send(page, 'Pricing');
      await expect.poll(() => posts(engine, '/turn').length).toBe(1);
      engine.update({ needsPerson: true, running: true, questions: [proposal()] });
      const card = page.getByRole('region', { name: 'Which pricing line should lead?' });
      await expect(async () => { await page.reload(); await expect(card).toBeVisible({ timeout: 1500 }); }).toPass({ timeout: 15_000 });
      const tray = page.getByRole('region', { name: 'Waiting on you' });
      await expect(tray.locator('.tray-crumb')).toHaveText(WORDS.proposalCrumb);
      await expect(tray.locator('.tray-crumb-kind')).toHaveText(WORDS.proposalCount);
      const choices = card.locator('.proposal-choice');
      await expect(choices.first().locator('.proposal-knows')).toHaveText(WORDS.proposalKnows);
      await expect(choices.nth(1).locator('.proposal-knows')).toHaveCount(0);
      const [a, b] = await choices.evaluateAll(els => els.map(el => el.getBoundingClientRect()));
      expect(a.top).toBe(b.top);
      expect(a.height).toBe(b.height);
      expect(Math.round(b.left - a.right)).toBe(6);
      await expect(card.locator('.proposal-choices')).toHaveCSS('column-gap', '6px');
      await expect(card.getByRole('button', { name: /^Agree/ })).toBeVisible();
      await expect(card.getByRole('button', { name: 'Choose Usage-based, from $0' })).toBeVisible();
      // Agree and Choose B are different payloads: only the second records an overrule.
      await card.getByRole('button', { name: /^Agree/ }).click();
      await expect.poll(() => posts(engine, '/answer').length).toBe(1);
      const agreed = posts(engine, '/answer')[0].body as Record<string, unknown>;
      expect(agreed).toMatchObject({ id: 31, key: 'a', picked: ['a'] });
      expect(agreed.overruled).toBeUndefined();
    });

    test('12c: Choose B sends the other key as overruled', async ({ page }) => {
      const base = pendingQuestion();
      const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
      await openApp(page);
      await send(page, 'Pricing');
      await expect.poll(() => posts(engine, '/turn').length).toBe(1);
      engine.update({ needsPerson: true, running: true, questions: [proposal()] });
      const card = page.getByRole('region', { name: 'Which pricing line should lead?' });
      await expect(async () => { await page.reload(); await expect(card).toBeVisible({ timeout: 1500 }); }).toPass({ timeout: 15_000 });
      await card.getByRole('button', { name: 'Choose Usage-based, from $0' }).click();
      await expect.poll(() => posts(engine, '/answer').length).toBe(1);
      expect(posts(engine, '/answer')[0].body).toMatchObject({ id: 31, key: 'b', picked: ['b'], overruled: true });
    });

    test('I2: Why? opens "Decided automatically" with By, Because and Sure, and Next time… calls the place decide route', async ({ page }) => {
      const bodies: { method: string; body: unknown }[] = [];
      await page.route('**/api/engine/places/marketing/decide', route => { bodies.push({ method: route.request().method(), body: route.request().postDataJSON() }); return route.fulfill({ json: {} }); });
      await openDetail(page);
      await page.getByRole('button', { name: 'Why?' }).click();
      const card = page.getByRole('dialog', { name: WORDS.whyTitle });
      await expect(card.locator('.why-fact')).toHaveText(['ByConfig parser', 'BecauseYou allowed this here 6 times. Reversible.', 'Sure94%']);
      await card.getByRole('radio', { name: 'Always ask me' }).click();
      await card.getByRole('radio', { name: 'Fine, keep deciding' }).click();
      await expect.poll(() => bodies.length).toBe(2);
      expect(bodies).toEqual([{ method: 'PUT', body: { alwaysAsk: true } }, { method: 'PUT', body: { alwaysAsk: false } }]);
    });

    test('I2: Overturn lists the tasks that used the decision, preselects nothing and posts the chosen handling', async ({ page }) => {
      const bodies: unknown[] = [];
      await page.route('**/api/engine/decisions/d1/overturn', route => { bodies.push(route.request().postDataJSON()); return route.fulfill({ json: {} }); });
      await openDetail(page);
      await page.getByRole('button', { name: 'Why?' }).click();
      await page.getByRole('button', { name: 'Overturn', exact: true }).click();
      const dialog = page.getByRole('dialog', { name: 'Overturn this decision' });
      await expect(dialog.locator('.overturn-title')).toHaveText(WORDS.overturnTitle);
      await expect(dialog.locator('.overturn-used')).toHaveText(WORDS.used);
      for (const name of ['Notify them', 'Pause them', 'Leave them']) await expect(dialog.getByRole('radio', { name })).toHaveAttribute('aria-checked', 'false');
      await dialog.getByRole('radio', { name: 'Pause them' }).click();
      await dialog.getByRole('button', { name: 'Overturn', exact: true }).click();
      await expect(page.getByRole('status')).toContainText('Decision overturned.');
      expect(bodies).toEqual([{ choice: 'pause' }]);
    });

    test('I2: with no dependents Overturn offers nothing to choose', async ({ page }) => {
      await openDetail(page, 0);
      await page.getByRole('button', { name: 'Why?' }).click();
      await page.getByRole('button', { name: 'Overturn', exact: true }).click();
      await expect(page.locator('.overturn-used')).toHaveCount(0);
      await expect(page.getByRole('radio', { name: 'Notify them' })).toHaveCount(0);
    });
  });
}

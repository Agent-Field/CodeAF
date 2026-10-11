import { test, expect, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';
import { openApp, posts, send } from './support/conversation';

const field = (page: Page) => page.locator('.composer-field');
const review = (id = 41): EngineQuestion => ({ id, kind: 'landing', ask: 'landing', head: `Review parser ${id}`, asker: { kind: 'task', name: `Review parser ${id}` }, reason: 'Parser changes are ready for your call.', options: [{ key: 'a', label: 'Accept' }, { key: 'n', label: 'Not right', safe: true }, { key: 's', label: 'Tell it' }] });
const clarification = (id = 42): EngineQuestion => ({ id, kind: 'ask', head: `Which customers ${id}?`, ask: 'clarification', input: { kind: 'text' } });
async function setup(page: Page, options: { draft?: string; fail?: boolean } = {}): Promise<MockEngine> {
  const base = pendingQuestion();
  const engine = await installMockEngine(page, { ...base, fail: options.fail ? { answer: 409 } : undefined, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
  await openApp(page);
  await send(page, 'Build parser');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  await expect(field(page)).toHaveValue('');
  if (options.draft) {
    await field(page).fill(options.draft);
    await expect.poll(() => page.evaluate(text => Object.values(localStorage).some(value => value.includes(text)), options.draft!)).toBe(true);
  }
  await expect.poll(() => page.evaluate(() => Object.values(localStorage).some(value => value.includes('mock-session-1.jsonl')))).toBe(true);
  engine.update({ needsPerson: true, running: false, questions: [review()] });
  await expect(page.locator('.decision-tray')).toBeVisible();
  return engine;
}

for (const scheme of ['light', 'dark'] as const) {
  test(`${scheme}: review words answer the card with no chat turn; layout has one header and action row`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
    const engine = await setup(page);
    await expect(field(page)).toHaveAccessibleName('Reply to Review parser 41');
    await expect(field(page)).toHaveAttribute('placeholder', 'Tell it what to change');
    await expect(page.locator('.decision-tray').getByText('Review parser 41', { exact: true })).toHaveCount(1);
    await expect(page.getByRole('button', { name: /Tell it/ })).toHaveCount(0);
    const tops = await page.locator('.tray-actions').evaluate(el => [...el.querySelectorAll('button')].map(button => button.getBoundingClientRect().top));
    expect(Math.max(...tops) - Math.min(...tops)).toBeLessThanOrEqual(1);
    await page.setViewportSize({ width: 320, height: 560 });
    expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth)).toBe(true);
    await expect(page.getByRole('button', { name: 'Later', exact: true })).toBeVisible();
    await field(page).fill('Keep the public API');
    await field(page).press('Enter');
    await expect.poll(() => posts(engine, '/answer').length).toBe(1);
    expect(posts(engine, '/answer')[0].body).toMatchObject({ kind: 'landing', id: 41, key: 'n', change: 'Keep the public API' });
    expect(posts(engine, '/turn')).toHaveLength(1);
    await expect(field(page)).toHaveValue('');
  });
}

test('earlier chat draft is offered, never hijacked; clicking the offer binds it', async ({ page }) => {
  const engine = await setup(page, { draft: 'My earlier draft' });
  await expect(field(page)).toHaveAccessibleName('Message');
  await expect(field(page)).toHaveValue('My earlier draft');
  await page.getByRole('button', { name: 'Reply to Review parser 41', exact: true }).click();
  await expect(field(page)).toHaveAccessibleName('Reply to Review parser 41');
  await field(page).press('Meta+Enter');
  await expect.poll(() => posts(engine, '/answer').length).toBe(1);
  expect(posts(engine, '/answer')[0].body.change).toBe('My earlier draft');
  expect(posts(engine, '/turn')).toHaveLength(1);
});

for (const dismiss of ['x', 'Escape']) {
  test(`${dismiss} unbinds; Enter sends a chat message`, async ({ page }) => {
    const engine = await setup(page);
    if (dismiss === 'x') await page.getByRole('button', { name: 'Dismiss reply' }).click();
    else await field(page).press('Escape');
    await expect(field(page)).toHaveAccessibleName('Message');
    await field(page).fill('Ordinary chat');
    await field(page).press('Enter');
    await expect.poll(() => posts(engine, '/turn').length).toBe(2);
    expect(posts(engine, '/answer')).toHaveLength(0);
  });
}

test('paging rebinds and Later on the last card unbinds; compact content is centred', async ({ page }) => {
  const engine = await setup(page);
  engine.update({ questions: [review(), clarification()] });
  await page.getByRole('button', { name: 'Next question', exact: true }).click();
  await expect(field(page)).toHaveAccessibleName('Reply to Which customers 42?');
  await page.getByRole('button', { name: 'Later', exact: true }).click();
  await expect(field(page)).toHaveAccessibleName('Reply to Review parser 41');
  await page.getByRole('button', { name: 'Later', exact: true }).click();
  await expect(field(page)).toHaveAccessibleName('Message');
  engine.update({ entries: [...engine.snapshot().entries, { Role: 'assistant', Answer: true, Text: Array.from({ length: 100 }, (_, i) => `Paragraph ${i}: parser details for scrolling.`).join('\n\n') }] });
  await expect(page.locator('.conversation-scroll')).toContainText('Paragraph 99');
  await page.locator('.conversation-scroll').evaluate(el => { el.scrollTop = 0; el.dispatchEvent(new Event('scroll')); });
  await expect(page.locator('.tray-compact')).toBeVisible();
  const geometry = await page.locator('.tray-compact').evaluate(el => {
    const bar = el.getBoundingClientRect(), label = el.querySelector('.tray-compact-count')!.getBoundingClientRect(), later = el.querySelector('.tray-compact-later')!.getBoundingClientRect();
    return { height: bar.height, label: Math.abs(label.y + label.height / 2 - bar.y - bar.height / 2), later: Math.abs(later.y + later.height / 2 - bar.y - bar.height / 2) };
  });
  expect(geometry.height).toBe(40);
  expect(geometry.label).toBeLessThanOrEqual(1);
  expect(geometry.later).toBeLessThanOrEqual(1);
});

test('rejected answer keeps the draft and announces failure', async ({ page }) => {
  const engine = await setup(page, { fail: true });
  await field(page).fill('Keep this reason');
  await field(page).press('Enter');
  await expect.poll(() => posts(engine, '/answer').length).toBe(1);
  await expect(field(page)).toHaveValue('Keep this reason');
  await expect(page.getByText('That did not go through. Try again.')).toBeVisible();
  expect(posts(engine, '/turn')).toHaveLength(1);
});

test('Other binds words explicitly and a permission reason uses the same composer', async ({ page }) => {
  const engine = await setup(page);
  engine.update({ questions: [{ id: 50, kind: 'ask', ask: 'choice', head: 'Which database?', options: [{ key: 'sqlite', label: 'SQLite' }, { key: 'other', label: 'Other' }] }] });
  await expect(field(page)).toHaveAccessibleName('Reply to Which database?');
  await page.getByRole('button', { name: 'Dismiss reply' }).click();
  await page.getByRole('button', { name: 'Other', exact: true }).click();
  expect(posts(engine, '/answer')).toHaveLength(0);
  await field(page).fill('Use Postgres');
  await field(page).press('Enter');
  await expect.poll(() => posts(engine, '/answer').length).toBe(1);
  expect(posts(engine, '/answer')[0].body).toMatchObject({ id: 50, key: 'other', change: 'Use Postgres' });
  engine.update({ questions: [{ id: 51, kind: 'consent', ask: 'permission', head: 'Allow the command?', options: [{ key: 'allow', label: 'Allow' }, { key: 'deny', label: 'Deny', safe: true }] }] });
  await expect(field(page)).toHaveAccessibleName('Message');
  await page.getByRole('button', { name: 'and say why', exact: true }).click();
  await expect(field(page)).toHaveAccessibleName('Reply to Allow the command?');
  await expect(page.locator('.decision-tray textarea')).toHaveCount(0);
  await field(page).fill('Use a safer command');
  await field(page).press('Enter');
  await expect.poll(() => posts(engine, '/answer').length).toBe(2);
  expect(posts(engine, '/answer')[1].body).toMatchObject({ id: 51, key: 'deny', change: 'Use a safer command' });
  expect(posts(engine, '/turn')).toHaveLength(1);
});

test('a draft started after answering one card is offered to the next card', async ({ page }) => {
  const engine = await setup(page);
  await field(page).fill('Keep ABI');
  await field(page).press('Enter');
  await expect(field(page)).toHaveAccessibleName('Message');
  await field(page).fill('My new chat draft');
  engine.update({ questions: [clarification()] });
  await expect(page.getByRole('button', { name: 'Reply to Which customers 42?', exact: true })).toBeVisible();
  await expect(field(page)).toHaveAccessibleName('Message');
  await field(page).press('Enter');
  await expect.poll(() => posts(engine, '/turn').length).toBe(2);
  expect(posts(engine, '/answer')).toHaveLength(1);
});

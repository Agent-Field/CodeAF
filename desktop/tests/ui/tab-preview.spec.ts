import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';

// Shell 2h "Preview", 3a, 3k: hovering an inactive tab for 500ms shows a 300px content column plus 14px side padding; moving across
// neighbours swaps it at once; a press, a drag or Escape closes it; a card that needs you carries Allow all.
const delay = design.interaction.previewOpenDelay;
const permission = (id: number, head: string, tasks = ['3']): EngineQuestion => ({
  id, kind: 'consent', ask: 'permission', head, batch: 'step:1',
  options: [{ key: '1', label: 'allow once' }, { key: '3', label: 'deny', safe: true }],
  blocking: { turn: true, tasks },
});
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, titleSource: 'manual', kind: 'conversation', draft: '', pinned: false, ...over });

async function seed(page: Page, tabs: Record<string, unknown>[]) {
  const state = { tabs, groups: [], closed: [], activeId: 'a', nextNumber: tabs.length + 1, recentIds: tabs.map(t => t.id) };
  await page.addInitScript(value => { localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}
const card = (page: Page, title: string) => page.getByRole('group', { name: `Preview of ${title}`, exact: true });

async function open(page: Page) {
  const base = plainReply();
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, needsPerson: true, tasks: [], questions: [...[1, 2, 3].map(n => permission(n, `Run git step ${n}`)), permission(7, 'Delete the build folder', ['9'])], entries: [{ Role: 'user', Text: 'Port the fix' }, { Role: 'assistant', Text: 'The fixtures run now.' }] } });
  await seed(page, [tab('a', 'Active tab', { sessionFile: 'a.jsonl' }), tab('b', 'Port fix', { kind: 'task', sessionFile: 'b.jsonl', route: { taskId: '3', back: [''], forward: [] } }), tab('c', 'Plain tab', { draft: 'A saved thought' })]);
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Port fix', exact: true })).toBeVisible();
  return engine;
}

test('a 500ms hover opens the reference 328px padded text card; hovering the active tab opens nothing', async ({ page }) => {
  await open(page);
  expect(delay).toBe(500);
  const port = page.getByRole('tab', { name: 'Port fix', exact: true });
  const started = Date.now();
  await port.hover();
  await page.waitForTimeout(delay * 0.4);
  await expect(card(page, 'Port fix')).toHaveCount(0);
  await expect(card(page, 'Port fix')).toBeVisible();
  expect(Date.now() - started).toBeGreaterThanOrEqual(delay * 0.9);
  expect(await card(page, 'Port fix').locator('.preview-card').evaluate(el => (el as HTMLElement).offsetWidth)).toBe(328);
  await expect(card(page, 'Port fix')).toContainText('Task');
  await expect(card(page, 'Port fix')).toContainText('Needs you');
  await expect(card(page, 'Port fix')).toContainText('Allow 3 actions?');
  await page.mouse.move(0, 0);
  await expect(card(page, 'Port fix')).toHaveCount(0);
  await page.getByRole('tab', { name: 'Active tab', exact: true }).hover();
  await page.waitForTimeout(delay * 1.5);
  await expect(card(page, 'Active tab')).toHaveCount(0);
});

test('moving across neighbouring tabs swaps the card at once, with no delay and no animation', async ({ page }) => {
  await open(page);
  await page.getByRole('tab', { name: 'Port fix', exact: true }).hover();
  await expect(card(page, 'Port fix')).toBeVisible();
  await page.getByRole('tab', { name: 'Plain tab', exact: true }).hover();
  // Far below the 500ms delay: the neighbour's card replaces the first immediately.
  await expect(card(page, 'Plain tab')).toBeVisible({ timeout: delay / 4 });
  await expect(card(page, 'Port fix')).toHaveCount(0);
  await expect(card(page, 'Plain tab')).toHaveAttribute('data-swap', 'true');
  await expect(card(page, 'Plain tab')).toContainText('Draft: A saved thought');
  await expect(card(page, 'Plain tab').getByRole('button')).toHaveCount(0);
});

test('a click, a press and Escape close the card, and it stays shut until the pointer leaves', async ({ page }) => {
  await open(page);
  const port = page.getByRole('tab', { name: 'Port fix', exact: true });
  await port.hover();
  await expect(card(page, 'Port fix')).toBeVisible();
  await port.hover();
  await page.mouse.down();
  await expect(card(page, 'Port fix')).toHaveCount(0);
  await page.mouse.up();
  await page.waitForTimeout(delay * 1.5);
  await expect(card(page, 'Port fix')).toHaveCount(0);
  await expect(port).toHaveAttribute('aria-selected', 'true');
  await page.mouse.move(0, 0);
  await page.getByRole('tab', { name: 'Plain tab', exact: true }).hover();
  await expect(card(page, 'Plain tab')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(card(page, 'Plain tab')).toHaveCount(0);
});

test('Allow all in the card answers every permission on the engine; Review opens the tab', async ({ page }) => {
  const engine = await open(page);
  await page.getByRole('tab', { name: 'Plain tab', exact: true }).click();
  await page.getByRole('tab', { name: 'Port fix', exact: true }).hover();
  const preview = card(page, 'Port fix');
  await expect(preview.getByRole('button', { name: 'Allow all', exact: true })).toBeVisible();
  // The card is a surface the pointer can enter: it stays open on the way to the button.
  await preview.getByRole('button', { name: 'Allow all', exact: true }).click();
  await expect.poll(() => engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/answer')).length).toBe(3);
  expect(engine.calls.filter(call => call.path.endsWith('/answer')).map(call => call.body.key)).toEqual(['1', '1', '1']);
  await expect(preview).toHaveCount(0);

  await page.mouse.move(0, 0);
  await page.getByRole('tab', { name: 'Active tab', exact: true }).click();
  await page.mouse.move(0, 0);
  await engine.update({ questions: [permission(9, 'Run once more')], needsPerson: true });
  await page.getByRole('tab', { name: 'Port fix', exact: true }).hover();
  await expect(card(page, 'Port fix').getByRole('button', { name: 'Allow', exact: true })).toBeVisible({ timeout: 6000 });
  await card(page, 'Port fix').getByRole('button', { name: 'Review', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Port fix', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(card(page, 'Port fix')).toHaveCount(0);
});

test('the card is a text surface: surface ground, radius 12, 13px title, 12px body', async ({ page }) => {
  await open(page);
  await page.getByRole('tab', { name: 'Plain tab', exact: true }).hover();
  const body = card(page, 'Plain tab').locator('.preview-card');
  await expect(body).toBeVisible();
  await expect(body).toHaveCSS('border-top-left-radius', '12px');
  await expect(body).toHaveCSS('padding', '12px 14px 14px');
  await expect(body).toHaveCSS('row-gap', '8px');
  await expect(body.locator('.preview-title')).toHaveCSS('font-size', '13px');
  await expect(body.locator('.preview-title')).toHaveCSS('font-weight', '500');
  await expect(body.locator('.preview-text')).toHaveCSS('font-size', '12px');
  await expect(body.locator('.preview-head')).toHaveCSS('font-size', '11px');
  await expect(body).toHaveCSS('box-shadow', /.+/);
});

test('the Design system page shows every preview card', async ({ page }) => {
  await page.goto('/');
  await openPage(page, 'Design system');
  const specimen = page.locator('[data-preview-specimen]');
  await expect(specimen.locator('.preview-card')).toHaveCount(10);
  await expect(specimen.getByRole('button', { name: 'Allow all', exact: true })).toHaveCount(2);
  await expect(specimen.getByRole('alert')).toContainText('Not sent.');
  await expect(specimen.locator('.preview-card-shot .preview-shot')).toHaveCSS('height', '120px');
});


test('a task card shows and answers only its own task questions, not the conversation\'s', async ({ page }) => {
  const engine = await open(page);
  await page.getByRole('tab', { name: 'Plain tab', exact: true }).click();
  await page.getByRole('tab', { name: 'Port fix', exact: true }).hover();
  const preview = card(page, 'Port fix');
  await expect(preview).toContainText('Allow 3 actions?');
  await expect(preview).not.toContainText('Delete the build folder');
  await preview.getByRole('button', { name: 'Allow all', exact: true }).click();
  await expect.poll(() => engine.calls.filter(call => call.path.endsWith('/answer')).length).toBe(3);
  // The other task's question was never answered from this card.
  expect(engine.calls.filter(call => call.path.endsWith('/answer')).map(call => call.body.id)).toEqual([1, 2, 3]);
});

test('a task with no question of its own does not say Needs you, whatever the conversation is asking', async ({ page }) => {
  const base = plainReply();
  await installMockEngine(page, { ...base, initial: { ...base.initial, needsPerson: true, tasks: [], questions: [permission(1, 'Run git step 1', ['3'])], entries: [{ Role: 'user', Text: 'Port the fix' }] } });
  await seed(page, [tab('a', 'Active tab'), tab('b', 'Other task', { kind: 'task', sessionFile: 'b.jsonl', route: { taskId: '5', back: [''], forward: [] } })]);
  await page.goto('/');
  await page.getByRole('tab', { name: 'Other task', exact: true }).hover();
  await expect(card(page, 'Other task')).toBeVisible();
  await expect(card(page, 'Other task')).not.toContainText('Needs you');
  await expect(card(page, 'Other task').getByRole('button')).toHaveCount(0);
});

test('a failed Allow all says so in the card, keeps asking, and can be tried again', async ({ page }) => {
  const base = plainReply();
  const scenario = { ...base, initial: { ...base.initial, needsPerson: true, tasks: [], questions: [permission(1, 'Run git step 1'), permission(2, 'Run git step 2')], entries: [{ Role: 'user' as const, Text: 'Port the fix' }] } };
  const engine = await installMockEngine(page, scenario);
  await seed(page, [tab('a', 'Active tab', { sessionFile: 'a.jsonl' }), tab('b', 'Port fix', { kind: 'task', sessionFile: 'b.jsonl', route: { taskId: '3', back: [''], forward: [] } })]);
  await page.goto('/');
  await expect(page.getByRole('tab', { name: 'Port fix', exact: true })).toBeVisible();
  scenario.fail = { answer: 500 };
  await page.getByRole('tab', { name: 'Port fix', exact: true }).hover();
  const preview = card(page, 'Port fix');
  await preview.getByRole('button', { name: 'Allow all', exact: true }).click();
  await expect(preview.getByRole('alert')).toContainText('Not sent.');
  await expect(preview).toContainText('Allow 2 actions?');
  await expect(preview.getByRole('button', { name: 'Allow all', exact: true })).toBeEnabled();
  await expect(preview.getByRole('button', { name: 'Review', exact: true })).toBeEnabled();
  scenario.fail = undefined;
  await preview.getByRole('button', { name: 'Allow all', exact: true }).click();
  await expect(preview).toHaveCount(0);
  expect(engine.calls.filter(call => call.path.endsWith('/answer')).length).toBeGreaterThanOrEqual(2);
});

test('file, diff and terminal cards read the target their tab carries, and it survives a reload', async ({ page }) => {
  const base = plainReply();
  await installMockEngine(page, {
    ...base,
    files: { 'internal/parse/lexer.go': { mime: 'text/plain', dataBase64: Buffer.from('package parse\n\nimport "io"\nvar a\n').toString('base64') } },
    terminals: [{ id: 'job-1', command: 'nightly-bench', title: 'nightly-bench', output: 'goos: darwin\r\nok codeaf/parse 4.2s\r\n' }],
  });
  const target = { sessionId: 'mock-1' };
  await seed(page, [tab('a', 'Active tab'), tab('f', 'lexer.go', { kind: 'file', target: { ...target, path: 'internal/parse/lexer.go' } }), tab('t', 'bench', { kind: 'terminal', target: { ...target, terminalId: 'job-1' } })]);
  await page.goto('/');
  for (const reload of [false, true]) {
    if (reload) await page.reload();
    await page.getByRole('tab', { name: 'lexer.go', exact: true }).hover();
    await expect(card(page, 'lexer.go')).toContainText('package parse');
    await expect(card(page, 'lexer.go')).toContainText('lines');
    await page.getByRole('tab', { name: 'bench', exact: true }).hover();
    await expect(card(page, 'bench')).toContainText('ok codeaf/parse 4.2s');
    await page.mouse.move(0, 0);
  }
});

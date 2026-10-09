import { test, expect, type Page } from '@playwright/test';
import type { EngineEntry, EngineQuestion, EngineSnapshot, EngineTaskRow } from '../../src/features/chat/engine-client';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { expectNoHorizontalOverflow, openApp, posts, send } from './support/conversation';

// Records shaped like real engine output (session.DisplayEntry, OutcomeWire):
// the parts of a conversation that are the engine talking to the model, or
// bookkeeping a person already sees elsewhere, never reach the page as-is.

type WireEntry = EngineEntry & { Took?: number; TaskIDs?: string[]; AsideKind?: string };

const narrate = (Text: string): WireEntry => ({ Role: 'assistant', Text, Answer: false });
const answer = (Text: string): WireEntry => ({ Role: 'assistant', Text, Answer: true });
const call = (CallID: string, Tool: string, args: object, Output: string, Took?: number): WireEntry => ({
  Role: 'tool', Text: '', Tool, Hint: Tool, CallID, Answered: true, Args: JSON.stringify(args), Output, Took,
});

// A hand-off landing as the session writes it to the model: batched under
// "while you worked:", an instruction lead, the task head with a transcript
// link, then the report.
const LANDING = [
  'while you worked:',
  '',
  'A note from the session, not from the person: work you handed off landed `done` — say that word back and no other, then answer the request it was for in its latest wording. Do not say again that it landed, and do not grade it.',
  'task 1 done: reading: List the files in this folder, read README if any, then c… · transcript file:///store/runs/s/tasks/1.jsonl',
  '`notes/hello.md` (3-line summary):',
  'Folder: a scratch workspace for trying the app.',
].join('\n');

const landingTurn: WireEntry[] = [
  narrate(''),
  call('l1', 'ls', { path: '/mock-workspace' }, 'notes/\nREADME.md'),
  narrate(''),
  call('r1', 'read', { path: '/mock-workspace/README.md' }, '# sandbox', 6_094_000_000),
  call('l2', 'ls', { path: '/mock-workspace/notes' }, 'hello.md', 6_094_000_000),
  { Role: 'aside', Text: LANDING, AsideKind: 'task', TaskIDs: ['1'] },
  narrate('done'),
  call('l3', 'ls', { path: '/mock-workspace/notes' }, 'hello.md'),
  answer('Created notes/hello.md with a 3-line summary.'),
];

async function sendOnce(page: Page, entries: WireEntry[], patch: Partial<EngineSnapshot> = {}): Promise<MockEngine> {
  const engine = await installMockEngine(page, { initial: { title: 'Records', entries: [] }, turns: [{ entries, patch }] });
  await openApp(page);
  await send(page, 'List the files, then write notes/hello.md');
  return engine;
}

test('a session note to the model is one quiet line with its report folded; no instruction or transcript path', async ({ page }) => {
  await sendOnce(page, landingTurn);
  await page.getByRole('button', { name: /^Worked \d+s · 3 steps · 4 calls$/ }).click();
  await expect(page.getByText('task 1 done: reading: List the files in this folder, read README if any, then c…')).toBeVisible();
  await expect(page.getByText(/A note from the session|file:\/\//)).toHaveCount(0);
  await page.getByRole('button', { name: 'Show', exact: true }).click();
  await expect(page.getByText(/Folder: a scratch workspace for trying the app\./)).toBeVisible();
  await expect(page.getByText(/A note from the session|file:\/\/|while you worked/)).toHaveCount(0);
});

test('a step is titled from its tools when the narration is a lone word; parallel calls time as the longest', async ({ page }) => {
  await sendOnce(page, landingTurn);
  // Two parallel calls of 6.1s each ran for 6.1s, not 12s.
  const summary = page.getByRole('button', { name: /^Worked 6s · 3 steps · 4 calls$/ });
  await summary.click();
  const steps = page.locator('.work-step-head');
  await expect(steps).toHaveCount(3);
  await expect(page.locator('.work-step-title').filter({ hasText: /^done$/ })).toHaveCount(0);
  await expect(page.locator('.work-step-title').nth(2)).toHaveText('Listed 1 item');
  const timed = steps.nth(1);
  await expect(timed.locator('.work-time')).toHaveText('6.1s');
  await timed.click();
  // Each call shows the duration the record kept for it.
  await expect(page.locator('.work-call .work-call-time')).toHaveText(['6.1s', '6.1s']);
});

const fileName: EngineQuestion = {
  id: 9,
  kind: 'ask',
  ask: 'choice',
  head: 'Which name should the new file have?',
  pick: { key: 'c', reason: 'Continues the src/ letter series.' },
  options: [
    { key: 'c', label: 'src/c.txt', body: 'Continues the src/ letter series already there.' },
    { key: 'n', label: 'notes.md', body: 'A plain notes file at the repo root.' },
  ],
};

/** Send once so the tab owns a session, then let the engine ask; reload reads the new state. */
async function openAsking(page: Page, questions: EngineQuestion[]): Promise<MockEngine> {
  const engine = await installMockEngine(page, { initial: { title: 'Records', entries: [] }, turns: [{ entries: [answer('Asking first.')] }] });
  await openApp(page);
  await send(page, 'Ask me which file name I prefer');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({ needsPerson: true, running: true, questions });
  await expect(async () => {
    await page.reload();
    await expect(page.getByRole('region', { name: 'Waiting on you' })).toBeVisible({ timeout: 1500 });
  }).toPass({ timeout: 15_000 });
  return engine;
}

test('option labels keep the engine\'s case for paths and file names', async ({ page }) => {
  await openAsking(page, [fileName]);
  const card = page.getByRole('region', { name: 'Which name should the new file have?' });
  // Choices are option cards (design v3 tray): each radio is named by its label first, in the engine's case.
  await expect(card.getByRole('radio', { name: /^src\/c\.txt\b/ })).toBeVisible();
  await expect(card.getByRole('radio', { name: /^notes\.md\b/ })).toBeVisible();
  await expect(card.getByText(/Src\/c\.txt|Notes\.md/)).toHaveCount(0);
});

const proposal = (id: number, title: string): EngineQuestion => ({
  id,
  kind: 'task',
  ask: 'confirmation',
  head: `Start a task: ${title}`,
  deadline: new Date(Date.now() + 120_000).toISOString(),
  pick: { key: '1' },
  options: [{ key: '1', label: 'start it' }, { key: '2', label: 'no', safe: true }],
});

const startedItself = (token: string) => ({
  kind: 'task',
  token,
  head: `Start a task: ${token}`,
  outcome: 'withdrawn',
  words: 'No longer needed — it started on its own, as the card said it would',
  by: 'engine',
  at: new Date().toISOString(),
});

test('proposals that started on their own leave no nameless rows, live or after reload', async ({ page }) => {
  const engine = await openAsking(page, [proposal(8, 'create src/a.txt'), proposal(9, 'create src/b.txt')]);
  await expect(page.getByRole('button', { name: /^Waiting on you: Start a task/ })).toHaveCount(2);
  engine.update({ needsPerson: false, running: false, questions: [], recentOutcomes: [startedItself('9'), startedItself('8')] } as Partial<EngineSnapshot>);
  await expect(page.getByRole('button', { name: /^Waiting on you: Start a task/ })).toHaveCount(0);
  await expect(page.getByText(/No longer needed/)).toHaveCount(0);
  await page.reload();
  await expect(page.getByText('Asking first.')).toBeVisible();
  await expect(page.getByText(/No longer needed/)).toHaveCount(0);
});

const LONG_TITLE = 'Create src/a.txt containing A and check it twice';
const doneRow: EngineTaskRow = { ID: '1', Title: LONG_TITLE, Status: 'done', Waits: [], Seat: 'worker', Steps: 2, Started: '2026-10-09T10:00:00Z', Ended: '2026-10-09T10:00:10Z' };

for (const scheme of ['light', 'dark'] as const) {
  test(`400px ${scheme}: a task notice keeps its title whole and lets the summary wrap`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await page.setViewportSize({ width: 400, height: 800 });
    const aside: WireEntry = { Role: 'aside', Text: `${LONG_TITLE} done · ran 10s · Created src/a.txt with contents exactly "A" and verified it.`, TaskIDs: ['1'] };
    await sendOnce(page, [aside, answer('Both files are created.')], { tasks: [doneRow] });
    const notice = page.locator('.task-notice').first();
    await expect(notice.getByRole('button', { name: 'Report' })).toBeVisible();
    const title = notice.locator('.task-notice-title');
    await expect(title).toHaveText(LONG_TITLE);
    // Whole: nothing of the title is clipped.
    expect(await title.evaluate((el) => el.scrollWidth <= el.clientWidth && el.scrollHeight <= el.clientHeight)).toBe(true);
    // The summary drops below the title instead of squeezing it.
    const [titleBox, summaryBox] = await Promise.all([title.boundingBox(), notice.locator('.task-notice-summary').boundingBox()]);
    expect(summaryBox!.y).toBeGreaterThanOrEqual(titleBox!.y + titleBox!.height - 1);
    await expectNoHorizontalOverflow(page);
  });
}

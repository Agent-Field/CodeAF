import { test, expect, type Page } from '@playwright/test';
import type { EngineEntry, EngineTaskRow } from '../../src/features/chat/engine-client';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { taskRows } from './support/scenarios';
import { openApp, send } from './support/conversation';

type Wire = EngineEntry & { Took?: number };
const narrate = (Text: string): Wire => ({ Role: 'assistant', Text, Answer: false });
const answer = (Text: string): Wire => ({ Role: 'assistant', Text, Answer: true });
const call = (CallID: string, Tool: string, extra: Partial<Wire> = {}): Wire => ({
  Role: 'tool', Text: '', Tool, Hint: Tool, CallID, Answered: true, Args: '{}', Output: '', Took: 400_000_000, ...extra,
});

const DEEP = '/mock-workspace/research/academic-profiles/parts/sections/raw/notes.md';

test('a file chip is one line, and shows the directory relative to the workspace', async ({ page }) => {
  const scenario: Scenario = {
    initial: { title: 'Chip', entries: [] },
    turns: [{ entries: [answer(`Read \`${DEEP}\` and \`/mock-workspace/README.md\`.`)] }],
    files: {
      [DEEP]: { mime: 'text/plain', dataBase64: Buffer.from('# notes').toString('base64') },
      '/mock-workspace/README.md': { mime: 'text/plain', dataBase64: Buffer.from('# readme').toString('base64') },
    },
  };
  await installMockEngine(page, scenario);
  await openApp(page);
  await send(page, 'where');
  const deep = page.locator('.answer-block .file-chip', { hasText: 'notes.md' });
  await expect(deep).toBeVisible();
  const height = (await deep.boundingBox())!.height;
  const dir = deep.locator('.file-chip-dir');
  await expect(dir).not.toContainText('/mock-workspace');
  await expect(dir).toContainText('research');
  // One line: the chip is as tall as one chip, and the directory text itself is a single line.
  expect(height).toBeLessThanOrEqual(28);
  const lineOf = (box: { height: number } | null) => box!.height;
  expect(lineOf(await dir.boundingBox())).toBeLessThanOrEqual(lineOf(await deep.locator('.file-chip-name').boundingBox()) + 2);
  expect(await dir.evaluate((el) => getComputedStyle(el).whiteSpace)).toBe('nowrap');
  // A file at the workspace root has no directory part at all.
  const root = page.locator('.answer-block .file-chip', { hasText: 'README.md' });
  await expect(root).toBeVisible();
  await expect(root.locator('.file-chip-dir')).toHaveCount(0);
});

async function startAllFinished(page: Page) {
  const done = taskRows.map((row): EngineTaskRow => ({ ...row, Status: 'done', Live: undefined, Note: undefined, Ended: row.Started }));
  await installMockEngine(page, {
    initial: { title: 'Done', entries: [] },
    turns: [{ entries: [answer('All done.')], patch: { tasks: done } }],
  });
  await openApp(page);
  await send(page, 'finish it all');
}

test('with every task finished the panel lists the finished families, not an empty middle', async ({ page }) => {
  await startAllFinished(page);
  const panel = page.getByRole('complementary', { name: 'Tasks' });
  await expect(panel).toBeVisible();
  await expect(panel.getByRole('button', { name: /^Finished/ })).toHaveAttribute('aria-expanded', 'true');
  await expect(panel.getByRole('button', { name: /^(?!Collapse|Expand).*Migrate the settings screen/ })).toBeVisible();
  // The person may still fold it away.
  await panel.getByRole('button', { name: /^Finished/ }).click();
  await expect(panel.getByRole('button', { name: /^Finished/ })).toHaveAttribute('aria-expanded', 'false');
});

test('with live work the Finished fold stays closed', async ({ page }) => {
  const mixed: EngineTaskRow[] = [
    { ...taskRows[1], Parent: undefined, Status: 'done', Title: 'Old finished job' },
    { ...taskRows[2], Parent: undefined, Status: 'running', Title: 'Still running job' },
  ];
  await installMockEngine(page, {
    initial: { title: 'Mixed', entries: [] },
    turns: [{ entries: [answer('Working.')], patch: { tasks: mixed } }],
  });
  await openApp(page);
  await send(page, 'go');
  const panel = page.getByRole('complementary', { name: 'Tasks' });
  await expect(panel.getByRole('button', { name: /Still running job/ })).toBeVisible();
  await expect(panel.getByRole('button', { name: /^Finished/ })).toHaveAttribute('aria-expanded', 'false');
  await expect(panel.getByRole('button', { name: /Old finished job/ })).toHaveCount(0);
});

test('a retried call is not counted failed, and a question wait is not worked time', async ({ page }) => {
  await installMockEngine(page, {
    initial: { title: 'Retry', entries: [] },
    turns: [{
      entries: [
        narrate('Proposing.'),
        call('p1', 'propose_task', { Failed: true, Took: 50_000_000, Output: 'bad plan' }),
        narrate('Again.'),
        call('p2', 'propose_task'),
        narrate('Asking.'),
        call('q1', 'ask', { Took: 82_000_000_000 }),
        answer('Done.'),
      ],
    }],
  });
  await openApp(page);
  await send(page, 'plan it');
  await expect(page.getByRole('button', { name: /^Worked 1s · 3 steps · 3 calls$/ })).toBeVisible();
});

import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { openApp, send, expectNoHorizontalOverflow } from './support/conversation';
import type { EngineQuestion, EngineTaskPage, EngineTaskRow } from '../../src/features/chat/engine-client';
import { expectAccessible } from './contracts';

const at = '2026-10-09T10:00:00Z';
const row = (ID: string, Title: string, Status: string, extra: Partial<EngineTaskRow> = {}): EngineTaskRow => ({
  ID, Title, Status, Waits: [], Seat: 'worker', Steps: 0, ...extra,
});

// One group of three: a finished task with every fact, a running one with none, one waiting on the person.
const rows: EngineTaskRow[] = [
  row('1', 'Ship the settings screen', 'running', { Started: at }),
  row('1.1', 'Read the current screen', 'done', { Parent: '1', Steps: 4, Started: at, Ended: at, Model: 'deepseek/deepseek-v4.1-flash', USD: 0.14, Tokens: 41000 }),
  row('1.2', 'Port the form fields', 'running', { Parent: '1', Live: { Step: 8, Command: 'npm run typecheck', Since: at } }),
  row('1.3', 'Update the snapshot tests', 'paused', { Parent: '1', Started: at }),
];
const waiting: EngineQuestion = {
  id: 9, kind: 'consent', ask: 'confirmation', head: 'Allow 3 git actions?', blocking: { tasks: ['1.3'] },
  options: [{ key: '1', label: 'allow once' }, { key: '3', label: 'deny', safe: true }],
};
const pageOf = (index: number, Description?: string): EngineTaskPage =>
  ({ Row: rows[index], Description, Result: '', Checks: [], Steps: [], Notes: [], Children: [], Folder: '/mock-workspace' }) as EngineTaskPage;

// A new tab has no session until the first send; the scripted reply then brings the plan rows and the question.
const scenario = () => ({
  initial: { title: 'Settings', entries: [] },
  turns: [{
    entries: [{ Role: 'assistant', Text: 'Working on it.', Answer: true }],
    patch: { tasks: rows, questions: [waiting] },
  }],
  taskPages: { '1.1': pageOf(1, 'Read the screen and list its form fields.'), '1.2': pageOf(2), '1.3': pageOf(3, 'Refresh the two snapshots.') },
}) as Scenario;

async function openExpanded(page: Page) {
  await installMockEngine(page, scenario());
  await openApp(page);
  await send(page, 'Ship it');
  await page.getByRole('complementary', { name: 'Tasks' }).getByRole('button', { name: 'Expand tasks' }).click();
}

const table = (page: Page) => page.getByRole('region', { name: 'Tasks' });
const rowButton = (page: Page, id: string) => table(page).locator(`.tasks-table-row[data-task-id="${id}"]`);
const detail = (page: Page, title: string) => page.getByRole('complementary', { name: `Task: ${title}` });
const filterTab = (page: Page, name: RegExp) => table(page).getByRole('group', { name: 'Filter tasks' }).getByRole('button', { name });

test('expand opens the table and Back leaves it', async ({ page }) => {
  await openExpanded(page);
  await expect(table(page)).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeHidden();
  await table(page).getByRole('button', { name: 'Back to the conversation' }).click();
  await expect(table(page)).toHaveCount(0);
  await expect(page.getByText('Working on it.')).toBeVisible();
});

test('Escape leaves the expanded view', async ({ page }) => {
  await openExpanded(page);
  await expect(table(page)).toBeVisible();
  await rowButton(page, '1.2').focus();
  await page.keyboard.press('Escape');
  await expect(table(page)).toHaveCount(0);
});

test('filter tabs show their counts and narrow the rows', async ({ page }) => {
  await openExpanded(page);
  const counts = { All: 4, 'Needs you': 1, Running: 2, Done: 1 };
  for (const [name, count] of Object.entries(counts)) {
    await expect(filterTab(page, new RegExp(`^${name}\\s*${count}$`))).toBeVisible();
  }
  await filterTab(page, /^Needs you/).click();
  await expect(rowButton(page, '1.3')).toBeVisible();
  await expect(rowButton(page, '1.1')).toHaveCount(0);
  await filterTab(page, /^Done/).click();
  await expect(rowButton(page, '1.1')).toBeVisible();
  await expect(rowButton(page, '1.3')).toHaveCount(0);
  await filterTab(page, /^Running/).click();
  await expect(rowButton(page, '1.2')).toBeVisible();
  await expect(rowButton(page, '1.1')).toHaveCount(0);
  await filterTab(page, /^All/).click();
  await expect(table(page).locator('.tasks-table-row')).toHaveCount(3);
});

test('selecting a row fills the detail pane; facts and instructions appear only when present', async ({ page }) => {
  await openExpanded(page);
  // The first waiting row is chosen at first, with the engine's question beside it.
  const waitingPane = detail(page, 'Update the snapshot tests');
  await expect(waitingPane).toBeVisible();
  await expect(waitingPane.getByText('Needs you')).toBeVisible();
  await expect(waitingPane.getByText('Allow 3 git actions?')).toBeVisible();

  await rowButton(page, '1.1').click();
  const done = detail(page, 'Read the current screen');
  await expect(done.getByRole('heading', { name: 'Read the current screen' })).toBeVisible();
  await expect(done.getByText('Done', { exact: true })).toBeVisible();
  await expect(done.getByText('Ship the settings screen')).toBeVisible();
  await expect(done.getByText('deepseek/deepseek-v4.1-flash')).toBeVisible();
  await expect(done.locator('dt')).toHaveText(['Model', 'Steps', 'Cost', 'Started']);
  await expect(done.getByText('Read the screen and list its form fields.')).toBeVisible();

  await rowButton(page, '1.2').click();
  const running = detail(page, 'Port the form fields');
  await expect(running.getByText('Running', { exact: true })).toBeVisible();
  await expect(running.locator('dt')).toHaveText(['Steps']);
  await expect(running.getByRole('region', { name: 'Instructions' })).toHaveCount(0);
});

test('the detail pane shows the task from the engine brief, with the full brief folded', async ({ page }) => {
  const brief = [
    'WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS',
    'This is the message this work came out of. Where anything below reads differently from it, their words are what was asked for.',
    '',
    'Ship it',
    '',
    'THE WORK',
    '',
    'Read the screen and list its form fields.',
    '',
    'DONE WHEN',
    '',
    'The fields are listed.',
  ].join('\n');
  const withBrief = scenario();
  withBrief.taskPages!['1.1'] = pageOf(1, brief);
  await installMockEngine(page, withBrief);
  await openApp(page);
  await send(page, 'Ship it');
  await page.getByRole('complementary', { name: 'Tasks' }).getByRole('button', { name: 'Expand tasks' }).click();
  await rowButton(page, '1.1').click();
  const pane = detail(page, 'Read the current screen');
  await expect(pane.getByText('Read the screen and list its form fields.')).toBeVisible();
  await expect(pane.getByText('WHAT THE PERSON ASKED FOR')).toHaveCount(0);
  await pane.getByRole('button', { name: 'Full brief' }).click();
  await expect(pane.getByRole('heading', { name: 'Done when' })).toBeVisible();
});

test('Open task goes to the task view', async ({ page }) => {
  await openExpanded(page);
  await rowButton(page, '1.1').click();
  await detail(page, 'Read the current screen').getByRole('button', { name: /^Open task/ }).click();
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toBeVisible();
  await expect(table(page)).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Read the current screen' })).toBeVisible();
});

test('reload keeps the expanded route and the selected row', async ({ page }) => {
  await openExpanded(page);
  await rowButton(page, '1.1').click();
  await expect(rowButton(page, '1.1')).toHaveAttribute('aria-current', 'true');
  await page.reload();
  await expect(table(page)).toBeVisible();
  await expect(rowButton(page, '1.1')).toHaveAttribute('aria-current', 'true');
  await expect(detail(page, 'Read the current screen')).toBeVisible();
});

test('400px dark: no horizontal overflow and accessible', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.setViewportSize({ width: 400, height: 800 });
  await installMockEngine(page, scenario());
  await openApp(page);
  await send(page, 'Ship it');
  // At this width the panel is a sheet, closed until the header toggle opens it.
  await page.getByRole('button', { name: /^Tasks · / }).click();
  await page.getByRole('button', { name: 'Expand tasks' }).click();
  await expect(table(page)).toBeVisible();
  await expectNoHorizontalOverflow(page);
  await expectAccessible(page);
});

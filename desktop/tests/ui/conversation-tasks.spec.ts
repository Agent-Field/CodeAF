import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { taskRows, withTasks } from './support/scenarios';
import type { Scenario } from './support/mock-engine';
import { openApp, posts, send } from './support/conversation';

const panel = (page: Page) => page.getByRole('complementary', { name: 'Tasks' });
const crumbs = (page: Page) => page.getByRole('navigation', { name: 'Breadcrumb' });

/** Send once; the scripted reply brings the aside, the plan rows and the answer. */
async function startWithTasks(page: Page, adjust: (scenario: Scenario) => void = () => undefined, noticeTaskId = '2') {
  const base = withTasks();
  adjust(base);
  // The shared fixture's aside text is not the "<title> <status> · <summary>" form the projection reads.
  const aside = { Role: 'aside', Text: 'Migrate the settings screen done · ran 30s · Two parts finished.', TaskIDs: [noticeTaskId] };
  const reply = [aside, base.initial.entries!.at(-1)!] as typeof base.initial.entries;
  const engine = await installMockEngine(page, {
    ...base,
    initial: { title: base.initial.title, entries: [] },
    turns: [{ entries: reply, patch: { tasks: base.initial.tasks } }],
  });
  await openApp(page);
  await expect(panel(page)).toHaveCount(0);
  await send(page, 'Migrate the settings screen');
  await expect(panel(page)).toBeVisible();
  return engine;
}

test('panel shows counts and nested rows, closes, and reopens from the header toggle', async ({ page }) => {
  await startWithTasks(page);
  const tasks = panel(page);
  // Every visible row counts once, parents included.
  await expect(tasks.getByText('1 of 4')).toBeVisible();
  await expect(tasks.getByRole('button', { name: /^(?!Collapse|Expand).*Migrate the settings screen/ })).toBeVisible();
  await expect(tasks.getByRole('button', { name: /^(?!Collapse|Expand).*Port the form fields/ })).toBeVisible();
  await tasks.getByRole('button', { name: 'Close tasks' }).click();
  await expect(tasks).toHaveCount(0);
  await page.getByRole('button', { name: /^Tasks · / }).click();
  await expect(tasks).toBeVisible();
});

test('a task notice opens the task in the same tab; Back and Ctrl+[ return', async ({ page }) => {
  await startWithTasks(page);
  const notice = () => page.locator('.turn-v2').getByRole('button', { name: /Migrate the settings screen/ });
  await notice().click();
  await expect(crumbs(page)).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Migrate the settings screen' })).toBeVisible();
  // The worker brief is a card under the title, clamped to three lines until Instructions is opened.
  const brief = page.getByText('Move the settings screen onto the shared form primitives.');
  await expect(brief).toBeVisible();
  const toggle = page.getByRole('button', { name: 'Instructions' });
  await expect(toggle).toHaveAttribute('aria-expanded', 'false');
  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('tab')).toHaveCount(1);
  await crumbs(page).getByRole('button').click();
  await expect(crumbs(page)).toHaveCount(0);
  await expect(page.getByText('I split the work into three parts.')).toBeVisible();
  await notice().click();
  await expect(crumbs(page)).toBeVisible();
  await page.keyboard.press('Control+BracketLeft');
  await expect(crumbs(page)).toHaveCount(0);
});

test('a task notice opens a background tab on modifier-click or middle-click, and its menu matches the row', async ({ page }) => {
  const engine = await startWithTasks(page);
  const row = page.locator('.turn-v2 .task-notice-row');
  await expect(page.getByRole('tab')).toHaveCount(1);
  await row.click({ modifiers: ['Control', 'Meta'] });
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(crumbs(page)).toHaveCount(0);
  await expect(page.getByRole('tab').first()).toHaveAttribute('aria-selected', 'true');
  await row.click({ button: 'middle' });
  await expect(page.getByRole('tab')).toHaveCount(3);
  await expect(crumbs(page)).toHaveCount(0);

  const notice = page.locator('.turn-v2 .task-notice');
  await notice.click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Migrate the settings screen actions' });
  await expect(menu.getByRole('menuitem', { name: 'Open in new tab' })).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: 'Stop' })).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: 'Pause' })).toHaveCount(0);
  await expect(menu.getByRole('menuitem', { name: 'Resume' })).toHaveCount(0);
  await menu.getByRole('menuitem', { name: 'Open in new tab' }).click();
  await expect(page.getByRole('tab')).toHaveCount(4);
  await expect(crumbs(page)).toHaveCount(0);

  await notice.click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Stop' }).click();
  await expect.poll(() => posts(engine, '/tasks/2/cancel').length).toBe(1);
});

test('a child task notice offers Pause while running and Resume once that row is paused', async ({ page }) => {
  const engine = await startWithTasks(page, () => undefined, '2.2');
  const notice = page.locator('.turn-v2 .task-notice');
  await notice.click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Port the form fields actions' });
  await expect(menu.getByRole('menuitem', { name: 'Open in new tab' })).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: 'Pause' })).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: 'Stop' })).toBeVisible();
  await expect(menu.getByRole('menuitem', { name: 'Resume' })).toHaveCount(0);
  await menu.getByRole('menuitem', { name: 'Pause' }).click();
  await expect.poll(() => posts(engine, '/tasks/2.2/pause').length).toBe(1);

  engine.update({ tasks: taskRows.map((row) => (row.ID === '2.2' ? { ...row, Status: 'paused', Paused: true } : row)) });
  // The notice mark reads the status word ("paused" is "Your call"); the menu reads the plan row, which is paused.
  await expect(notice.getByRole('img', { name: 'Your call' })).toBeVisible();
  await notice.click({ button: 'right' });
  const paused = page.getByRole('menu', { name: 'Port the form fields actions' });
  await expect(paused.getByRole('menuitem', { name: 'Resume' })).toBeVisible();
  await expect(paused.getByRole('menuitem', { name: 'Pause' })).toHaveCount(0);
  await paused.getByRole('menuitem', { name: 'Resume' }).click();
  await expect.poll(() => posts(engine, '/tasks/2.2/resume').length).toBe(1);
});

test('modifier-click on a task row opens a background tab', async ({ page }) => {
  await startWithTasks(page);
  await expect(page.getByRole('tab')).toHaveCount(1);
  await panel(page).getByRole('button', { name: /^(?!Collapse|Expand).*Port the form fields/ }).click({ modifiers: ['Control', 'Meta'] });
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(crumbs(page)).toHaveCount(0);
  await expect(page.getByRole('tab').first()).toHaveAttribute('aria-selected', 'true');
});

test('a note sent from the task view reaches the task and shows as the person\'s note', async ({ page }) => {
  const engine = await startWithTasks(page);
  await page.locator('.turn-v2').getByRole('button', { name: /Migrate the settings screen/ }).click();
  const note = page.getByRole('textbox', { name: 'Note to this task' });
  await note.fill('also cover the empty case');
  await page.getByRole('button', { name: 'Send note' }).click();
  await expect.poll(() => posts(engine, '/tasks/2/note').length).toBe(1);
  expect(posts(engine, '/tasks/2/note')[0].body).toEqual({ text: 'also cover the empty case' });
  await expect(page.getByRole('list', { name: 'Notes' })).toContainText('also cover the empty case');
});

test('a running task row pauses its task through the engine', async ({ page }) => {
  const engine = await startWithTasks(page);
  const row = panel(page).locator('.task-row', { has: page.locator('[data-task-id="2.2"]') });
  await row.hover();
  await row.getByRole('button', { name: 'Pause' }).click();
  await expect.poll(() => posts(engine, '/pause').length).toBe(1);
  expect(posts(engine, '/pause')[0].path).toMatch(/\/sessions\/mock-1\/tasks\/2\.2\/pause$/);
});

const openTask = (page: Page) => page.locator('.turn-v2').getByRole('button', { name: /Migrate the settings screen/ }).click();

// The engine's own brief layout (task_brief.go in the session package), header text included.
const WORKER_BRIEF = [
  'WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS',
  'This is the message this work came out of. Where anything below reads differently from it, their words are what was asked for.',
  '',
  'Migrate the settings screen and keep the tests green.',
  '',
  'THE WORK',
  '',
  'Move the settings screen onto the shared form primitives.',
  '',
  'WHAT TO PRODUCE',
  '',
  'src/settings.tsx using the shared primitives.',
].join('\n');

test('the Instructions card shows the task, not the engine brief; the full brief is one click away', async ({ page }) => {
  await startWithTasks(page, (scenario) => { scenario.taskPages!['2'].Description = WORKER_BRIEF; });
  await openTask(page);
  const card = page.getByRole('region', { name: 'Instructions' });
  await expect(card.getByText('Move the settings screen onto the shared form primitives.')).toBeVisible();
  await expect(card.getByText('WHAT THE PERSON ASKED FOR')).toHaveCount(0);
  await card.getByRole('button', { name: 'Instructions' }).click();
  await card.getByRole('button', { name: 'Full brief' }).click();
  await expect(card.getByRole('heading', { name: 'What to produce' })).toBeVisible();
  await expect(card.getByText('src/settings.tsx using the shared primitives.')).toBeVisible();
});

test('a landing note reads as a result with the branch in secondary text', async ({ page }) => {
  await startWithTasks(page, (scenario) => {
    scenario.taskPages!['2'].Notes = [{ Author: '2', Body: 'landed on task/create-src-a3-txt-d3389b: 2 files', At: '2026-10-09T10:00:00Z' }];
  });
  await openTask(page);
  const notes = page.getByRole('list', { name: 'Notes' });
  await expect(notes.getByText('Landed · 2 files')).toBeVisible();
  await expect(notes.getByText('task/create-src-a3-txt-d3389b')).toBeVisible();
  await expect(notes.getByText(/^landed on /)).toHaveCount(0);
});

test('a note the engine refuses is shown, and the draft stays', async ({ page }) => {
  const engine = await startWithTasks(page, (scenario) => { scenario.fail = { task: 409 }; });
  await openTask(page);
  const field = page.getByRole('textbox', { name: 'Note to this task' });
  await field.fill('are you there');
  await page.getByRole('button', { name: 'Send note' }).click();
  await expect.poll(() => posts(engine, '/tasks/2/note').length).toBe(1);
  await expect(page.locator('.task-composer-error')).toContainText('Mock engine forced task failure');
  await expect(field).toHaveValue('are you there');
});

test('the task view follows the panel: an ended task says so and cannot take notes', async ({ page }) => {
  const engine = await startWithTasks(page);
  await openTask(page);
  await expect(page.getByRole('textbox', { name: 'Note to this task' })).toBeEnabled();
  engine.update({ tasks: taskRows.map((row) => (row.ID === '2' ? { ...row, Status: 'done', Ended: '2026-10-09T10:00:08Z' } : row)) });
  const field = page.getByRole('textbox', { name: 'Note to this task' });
  await expect(field).toBeDisabled();
  await expect(page.getByText('This task has finished, so it cannot read a note.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send note' })).toBeDisabled();
  await expect(page.locator('.task-view-body').getByText('Done', { exact: true })).toBeVisible();
});

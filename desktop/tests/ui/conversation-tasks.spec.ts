import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { withTasks } from './support/scenarios';
import { openApp, posts, send } from './support/conversation';

const panel = (page: Page) => page.getByRole('complementary', { name: 'Tasks' });
const crumbs = (page: Page) => page.getByRole('navigation', { name: 'Breadcrumb' });

/** Send once; the scripted reply brings the aside, the plan rows and the answer. */
async function startWithTasks(page: Page) {
  const base = withTasks();
  // The shared fixture's aside text is not the "<title> <status> · <summary>" form the projection reads.
  const aside = { Role: 'aside', Text: 'Migrate the settings screen done · ran 30s · Two parts finished.', TaskIDs: ['2'] };
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

test('panel shows counts and nested rows, closes, and reopens from the composer toggle', async ({ page }) => {
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
  await crumbs(page).getByRole('button', { name: 'Back' }).click();
  await expect(crumbs(page)).toHaveCount(0);
  await expect(page.getByText('I split the work into three parts.')).toBeVisible();
  await notice().click();
  await expect(crumbs(page)).toBeVisible();
  await page.keyboard.press('Control+BracketLeft');
  await expect(crumbs(page)).toHaveCount(0);
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

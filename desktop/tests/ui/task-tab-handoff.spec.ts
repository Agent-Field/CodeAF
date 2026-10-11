import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { withTasks } from './support/scenarios';
import { openApp, send } from './support/conversation';

const transcript = 'I split the work into three parts.';
const taskName = 'Migrate the settings screen';
const crumbs = (root: Page | Locator) => root.getByRole('navigation', { name: 'Breadcrumb' });
const taskRow = (page: Page) => page.getByRole('complementary', { name: 'Tasks' }).getByRole('button', { name: /^(?!Collapse|Expand).*Migrate the settings screen/ });

/** A chat drilled into the task the panel row names. The transcript is not on screen. */
async function drill(page: Page) {
  const base = withTasks();
  const aside = { Role: 'aside', Text: 'Migrate the settings screen done · ran 30s · Two parts finished.', TaskIDs: ['2'] };
  const reply = [aside, base.initial.entries!.at(-1)!] as typeof base.initial.entries;
  await installMockEngine(page, {
    ...base,
    initial: { title: base.initial.title, entries: [] },
    turns: [{ entries: reply, patch: { tasks: base.initial.tasks } }],
  });
  await openApp(page);
  await send(page, 'Migrate the settings screen');
  await expect(page.getByRole('complementary', { name: 'Tasks' })).toBeVisible();
  await taskRow(page).click();
  await expect(crumbs(page).locator('.back-header-current')).toHaveText(taskName);
  await expect(page.getByText(transcript)).toHaveCount(0);
}

/** The chat tab is the transcript again, and the task tab still shows the task after Back. */
async function expectHandedTab(page: Page) {
  await expect(page.getByRole('tab', { name: 'Settings migration', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByText(transcript)).toBeVisible();
  await expect(crumbs(page)).toHaveCount(0);

  const taskTab = page.getByRole('tab', { name: taskName, exact: true });
  await taskTab.click();
  await expect(page.getByRole('heading', { name: taskName })).toBeVisible();
  await expect(page.getByText(transcript)).toHaveCount(0);
  const parent = crumbs(page).locator('.back-header-parent');
  if (await parent.count()) await parent.click();
  await taskTab.click();
  await expect(page.getByRole('heading', { name: taskName })).toBeVisible();
  await expect(page.getByText(transcript)).toHaveCount(0);
  await expect(crumbs(page).locator('.back-header-current')).toHaveText(taskName);
}

test('expanding the tasks panel while a task is open hands that task to a new tab', async ({ page }) => {
  await drill(page);
  await page.getByRole('button', { name: 'Expand tasks' }).click();
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expectHandedTab(page);
});

test('Open in new tab on the drilled task hands it over', async ({ page }) => {
  await drill(page);
  await taskRow(page).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Open in new tab' }).click();
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expectHandedTab(page);
});

test('cmd-click on the drilled task hands it over', async ({ page }) => {
  await drill(page);
  await taskRow(page).click({ modifiers: ['Meta'] });
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expectHandedTab(page);
});

/** Starts dragging the tab with this name and leaves the button down at `to`. */
async function dragTab(page: Page, name: string, to: { x: number; y: number }) {
  const box = (await page.getByRole('tab', { name, exact: true }).boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 8, box.y + box.height / 2 + 24, { steps: 3 });
  await page.mouse.move(to.x, to.y, { steps: 8 });
  await page.mouse.move(to.x + 1, to.y);
}

test('dragging a drilled chat into a split hands the task to a new pane', async ({ page }) => {
  await drill(page);
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
  const area = (await page.locator('.workspace-conversation').boundingBox())!;
  await dragTab(page, 'Settings migration', { x: area.x + area.width - 20, y: area.y + area.height / 2 });
  await page.mouse.up();

  const conversation = page.locator('.workspace-pane', { hasText: transcript });
  const task = page.locator('.workspace-pane', { has: page.getByRole('heading', { name: taskName }) });
  await expect(conversation).toHaveCount(1);
  await expect(crumbs(conversation)).toHaveCount(0);
  await expect(task).toHaveCount(1);
  await expect(task.getByText(transcript)).toHaveCount(0);

  const parent = crumbs(task).locator('.back-header-parent');
  if (await parent.count()) await parent.click();
  await expect(task.getByRole('heading', { name: taskName })).toBeVisible();
  await expect(task.getByText(transcript)).toHaveCount(0);
  await expect(crumbs(task).locator('.back-header-current')).toHaveText(taskName);
});

import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { withTasks } from './support/scenarios';
import { openApp, send } from './support/conversation';

const crumbs = (root: Page | Locator) => root.getByRole('navigation', { name: 'Breadcrumb' });

/** A chat with a running task, still on the transcript. The task tab is opened from that chat and does not take focus. */
async function chatBesideTask(page: Page) {
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
  await page.locator('.turn-v2 .task-notice').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Open in new tab' }).click();
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(crumbs(page)).toHaveCount(0);

  const chat = page.getByRole('tab', { name: 'Settings migration', exact: true });
  await expect(chat).toBeVisible();
  await chat.click({ button: 'right' });
  const menu = page.getByRole('menu', { name: 'Actions for Settings migration', exact: true });
  await menu.getByRole('menuitem', { name: 'Open in split' }).hover();
  await page.getByRole('menuitem', { name: 'Migrate the settings screen', exact: true }).click();
  await expect(page.getByRole('tabpanel')).toHaveAttribute('data-layout', '1x2');

  const left = page.locator('.workspace-pane').nth(0);
  const right = page.locator('.workspace-pane').nth(1);
  await expect(left).toHaveAttribute('aria-label', 'Settings migration');
  await expect(right).toHaveAttribute('aria-label', 'Migrate the settings screen');
  return { left, right };
}

test('a chat and the task opened from it keep independent views in a side-by-side split', async ({ page }) => {
  const { left, right } = await chatBesideTask(page);

  await expect(left.getByText('I split the work into three parts.')).toBeVisible();
  await expect(crumbs(left)).toHaveCount(0);
  await expect(crumbs(right).locator('.back-header-current')).toHaveText('Migrate the settings screen');
  await expect(right.getByRole('button', { name: 'Instructions' })).toBeVisible();
  await expect(right.getByText('I split the work into three parts.')).toHaveCount(0);

  await right.getByRole('region', { name: 'Tasks inside this one' }).getByRole('button', { name: /Port the form fields/ }).click();
  await expect(crumbs(right).locator('.back-header-current')).toHaveText('Port the form fields');
  await expect(crumbs(left)).toHaveCount(0);
  await expect(left.getByText('I split the work into three parts.')).toBeVisible();

  const rightTask = crumbs(right).locator('.back-header-current');
  await expect(rightTask).toHaveText('Port the form fields');
  await crumbs(right).locator('.back-header-parent').click();
  await expect(crumbs(left)).toHaveCount(0);
  await expect(left.getByText('I split the work into three parts.')).toBeVisible();
  await expect(right.getByText('I split the work into three parts.')).toHaveCount(0);
  const afterBack = (await rightTask.textContent()) ?? '';
  expect(afterBack.length).toBeGreaterThan(0);

  // The first pointer-down on an unfocused pane only moves focus. Drill on the next click.
  await left.locator('.pane-header').click();
  await expect(left).toHaveAttribute('data-focused', 'true');
  await left.locator('.turn-v2 .task-notice-row').click();
  await expect(crumbs(left).locator('.back-header-current')).toHaveText('Migrate the settings screen');
  await expect(rightTask).toHaveText(afterBack);
  await expect(right.getByText('I split the work into three parts.')).toHaveCount(0);
});

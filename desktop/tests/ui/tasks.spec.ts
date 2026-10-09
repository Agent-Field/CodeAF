import { expect, test } from '@playwright/test';
import { planForest, taskStanding, type PlanTaskRow } from '../../src/features/tasks/plan-model';

test('task standing preserves holds, stop, pause and waiting independently of status', () => {
 const row: PlanTaskRow = { ID: 'part', Title: 'Part', Status: 'ready' };
 expect(taskStanding(row).label).toBe('Ready');
 expect(taskStanding({ ...row, Status: 'claimed' }).label).toBe('Assigned');
 expect(taskStanding({ ...row, Status: 'running' }).label).toBe('Running');
 expect(taskStanding({ ...row, Hold: 'Awaiting machine admission' }).label).toBe('Queued');
 expect(taskStanding({ ...row, Paused: true }).attention).toBe(true);
 expect(taskStanding({ ...row, Waiting: true }).label).toBe('Waiting');
 expect(taskStanding({ ...row, Stopped: true, Paused: true }).label).toBe('Stopped');
 expect(taskStanding({ ...row, Interrupted: true }).label).toBe('Interrupted');
 expect(taskStanding({ ...row, Status: 'done' })).toEqual({ label: 'Completed', phase: 'completed', attention: false });
 expect(taskStanding({ ...row, Status: 'a-new-engine-state' }).label).toBe('State unavailable');
});

test('task containment does not convert prerequisite dependencies into parents', () => {
 const root = { ID: 'root', Title: 'Root', Status: 'running' };
 const first = { ID: 'first', Title: 'First', Status: 'done', Parent: 'root' };
 const second = { ID: 'second', Title: 'Second', Status: 'pending', Parent: 'root', Waits: ['first'] };
 const forest = planForest([root, first, second]);
 expect(forest.roots).toEqual([root]);
 expect(forest.children.get('root')).toEqual([first, second]);
 expect(forest.children.has('first')).toBe(false);
});

test('malformed parent cycles and unknown parents remain discoverable', () => {
 const orphan = { ID: 'orphan', Title: 'Orphan', Status: 'pending', Parent: 'absent' };
 const a = { ID: 'a', Title: 'A', Status: 'running', Parent: 'b' };
 const b = { ID: 'b', Title: 'B', Status: 'pending', Parent: 'a' };
 const forest = planForest([a, b, orphan]);
 expect(forest.roots.map(row => row.ID)).toEqual(['orphan', 'a']);
 expect(forest.children.get('a')?.map(row => row.ID)).toEqual(['b']);
});

import { expectAccessible, expectNoUnstyledControls, expectThemedSurface } from './contracts';
import type { Page } from '@playwright/test';
async function previewPlan(page: Page) {
 await page.getByRole('button', { name: 'UI preview', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Preview task plan', exact: true }).click();
}

test('real empty plan stays honest; preview tasks disclose their origin and containment', async ({ page }) => {
 await page.setViewportSize({ width: 1200, height: 800 }); await page.goto('/');
 await page.getByRole('button', { name: 'Plan', exact: true }).click();
 const plan = page.getByRole('complementary', { name: 'Conversation task plan' });
 await expect(plan).toContainText('No tasks in the current plan.');
 await expect(plan.getByRole('list', { name: 'Conversation tasks' }).getByRole('listitem')).toHaveCount(0);
 await page.getByRole('button', { name: 'Close task plan' }).click();
 await previewPlan(page);
 await expect(plan).toContainText('Task plan preview · no work is running');
 await expect(plan).toContainText('1 of 4 leaf tasks completed');
 await expect(plan.getByRole('button', { name: 'Review the changes, Completed', exact: true })).toBeVisible();
 const root = plan.getByRole('button', { name: 'Collapse Prepare the release', exact: true });
 await root.click();
 await expect(plan.getByRole('button', { name: 'Prepare the package, Waiting on prerequisites', exact: true })).not.toBeVisible();
 await plan.getByRole('button', { name: 'Expand Prepare the release', exact: true }).click();
 await plan.getByRole('button', { name: 'Prepare the package, Waiting on prerequisites', exact: true }).click();
 const details = plan.getByRole('region', { name: 'Details for Prepare the package' });
 await expect(details).toContainText('Waiting on');
 await expect(details.getByRole('listitem')).toHaveText(['Check cross-platform behavior', 'Confirm the release notes']);
 // Dependencies remain sibling tasks under the release, rather than becoming containment children.
 const branch = plan.locator('.task-branch').filter({ has: plan.getByRole('button', { name: 'Prepare the package, Waiting on prerequisites', exact: true }) }).last();
 await expect(branch.locator('.task-children')).toHaveCount(0);
 await plan.getByRole('button', { name: 'Review the changes, Completed', exact: true }).click();
 await expect(plan.getByRole('region', { name: 'Details for Review the changes' })).toContainText('Example review completed.');
 await expectAccessible(page); await expectNoUnstyledControls(page);
});

test('plan visibility and fixture are isolated per conversation and survive reload', async ({ page }) => {
 await page.setViewportSize({ width: 1200, height: 800 }); await page.goto('/'); await previewPlan(page);
 await page.getByRole('button', { name: 'New tab', exact: true }).first().click();
 await expect(page.getByRole('complementary', { name: 'Conversation task plan' })).not.toBeVisible();
 await page.getByRole('button', { name: 'Plan', exact: true }).click();
 await expect(page.getByRole('complementary', { name: 'Conversation task plan' })).toContainText('No tasks in the current plan.');
 await page.getByRole('button', { name: 'Close task plan' }).click();
 await page.getByRole('tab', { name: 'New conversation', exact: true }).click();
 await expect(page.getByRole('complementary', { name: 'Conversation task plan' })).toContainText('Task plan preview · no work is running');
 await page.reload();
 await expect(page.getByRole('complementary', { name: 'Conversation task plan' })).toContainText('Task plan preview · no work is running');
 await page.getByRole('button', { name: 'Close task plan' }).click();
 await expect(page.getByRole('button', { name: 'Plan', exact: true })).toHaveAttribute('aria-expanded', 'false');
});

for (const theme of ['light', 'dark'] as const) {
 test(`${theme}: narrow task drawer is themed, accessible and restores focus without squeezing work`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
  await page.setViewportSize({ width: 320, height: 560 }); await page.goto('/'); await previewPlan(page);
  const drawer = page.getByRole('dialog', { name: 'Conversation task plan' });
  await expect(drawer).toBeVisible(); await expectThemedSurface(page, drawer);
  await expect(page.getByRole('button', { name: 'Close task plan' })).toBeFocused();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await drawer.getByRole('button', { name: 'Confirm the release notes, Needs your input', exact: true }).click();
  await expect(drawer.getByRole('region', { name: 'Details for Confirm the release notes' })).toContainText('Choose which changes belong in this release.');
  await expectAccessible(page); await expectNoUnstyledControls(page);
  await page.keyboard.press('Escape'); await expect(drawer).not.toBeVisible();
  const trigger = page.getByRole('button', { name: 'Plan', exact: true });
  await trigger.click(); await expect(drawer).toBeVisible();
  await page.keyboard.press('Escape'); await expect(trigger).toBeFocused();
  await expect(page.getByRole('textbox', { name: /Draft for/ })).toBeInViewport();
 });
}

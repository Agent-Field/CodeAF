import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { withTaskTree } from './support/scenarios';
import { openApp, send } from './support/conversation';
import { tokenColor } from './contracts';

// Design 1a to 1d, Components "Row · tree, table" and "Row actions", 1f masks: the panel, its header and its rows.

const panel = (page: Page) => page.getByRole('complementary', { name: 'Tasks' });
const toggle = (page: Page) => page.getByRole('button', { name: /^Tasks · / });
const row = (page: Page, id: string) => panel(page).locator('.task-row', { has: page.locator(`[data-task-id="${id}"]`) });

async function start(page: Page, scheme: 'light' | 'dark' = 'light', size = { width: 1280, height: 800 }) {
  await page.emulateMedia({ colorScheme: scheme });
  await page.setViewportSize(size);
  const base = withTaskTree();
  const entries = base.initial.entries as never;
  await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [] }, turns: [{ entries, patch: { tasks: base.initial.tasks } }] });
  await openApp(page);
  await send(page, 'Ship trailing-comma support');
}

for (const scheme of ['light', 'dark'] as const) {
  test(`${scheme}: the header names what is running and what needs you, and the toggle shows and hides the panel`, async ({ page }) => {
    await start(page, scheme);
    const bar = page.locator('.conversation-bar');
    await expect(bar.getByText('Trailing commas')).toBeVisible();
    await expect(bar.getByText(/^\d+ running$/)).toBeVisible();
    await expect(bar.getByText('1 needs you here')).toBeVisible();
    // The old composer chip is gone: one toggle, in the header.
    await expect(page.locator('.composer-tasks')).toHaveCount(0);
    await expect(toggle(page)).toHaveAttribute('aria-pressed', 'true');
    await toggle(page).click();
    await expect(panel(page)).toHaveCount(0);
    await expect(toggle(page)).toHaveAttribute('aria-pressed', 'false');
    await toggle(page).click();
    await expect(panel(page)).toBeVisible();
  });

  test(`${scheme}: a row's rest, hover, press and selected fills follow the interaction law`, async ({ page }) => {
    await start(page, scheme);
    const target = row(page, '3.3');
    const main = target.locator('.task-panel-main');
    const field = await tokenColor(page, 'field');
    const field2 = await tokenColor(page, 'field-2');
    await expect(target).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await target.hover();
    await expect(target).toHaveCSS('background-color', field);
    await page.mouse.down();
    await expect(target).toHaveCSS('background-color', field2);
    await page.mouse.up();
    // A mouse click leaves no focus ring on the row.
    await expect(main).not.toHaveCSS('box-shadow', /oklch|rgb\(\d+, \d+, \d+\) 0px 0px 0px 2px/);
    await expect(main).toHaveAttribute('aria-current', 'true');
    await page.mouse.move(0, 0);
    await expect(target).toHaveCSS('background-color', field);
  });
}

test('hover shows open, pause and a more menu holding Stop; a settled row shows only open', async ({ page }) => {
  await start(page);
  const running = row(page, '3.1');
  await running.hover();
  await expect(running.getByRole('button', { name: 'Open in new tab' })).toBeVisible();
  await expect(running.getByRole('button', { name: 'Pause' })).toBeVisible();
  await expect(running.getByRole('button', { name: 'Stop' })).toHaveCount(0);
  await running.getByRole('button', { name: 'More actions' }).click();
  await expect(page.getByRole('menuitem', { name: 'Stop' })).toBeVisible();
  await page.keyboard.press('Escape');
  const settled = row(page, '3.3');
  await settled.hover();
  await expect(settled.getByRole('button', { name: 'Open in new tab' })).toBeVisible();
  await expect(settled.getByRole('button', { name: 'More actions' })).toHaveCount(0);
});

test('a finished task with no measurable time shows no age', async ({ page }) => {
  await start(page);
  await expect(row(page, '3.3').locator('.task-row-meta')).toHaveCount(0);
});

test('the list masks an edge only while that edge has more beyond it; the header and strip stay put', async ({ page }) => {
  await start(page, 'light', { width: 1280, height: 420 });
  const scroll = panel(page).locator('.task-panel-scroll');
  await expect(scroll).toHaveAttribute('data-below', 'true');
  await expect(scroll).not.toHaveAttribute('data-above', 'true');
  const top = (await panel(page).locator('.task-panel-header').boundingBox())!.y;
  await scroll.evaluate((node) => node.scrollTo(0, node.scrollHeight));
  await expect(scroll).toHaveAttribute('data-above', 'true');
  await expect(scroll).not.toHaveAttribute('data-below', 'true');
  expect((await panel(page).locator('.task-panel-header').boundingBox())!.y).toBe(top);
  await expect(scroll).toHaveCSS('mask-image', /linear-gradient/);
});

test('a narrow pane opens the panel as a sheet over a backdrop; the header toggle opens it and Escape closes it', async ({ page }) => {
  await start(page, 'light', { width: 900, height: 700 });
  await expect(panel(page)).toHaveCount(0);
  await expect(toggle(page)).toHaveAttribute('aria-pressed', 'false');
  await toggle(page).click();
  await expect(panel(page)).toHaveAttribute('data-variant', 'sheet');
  await expect(page.locator('.task-panel-backdrop')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(panel(page)).toHaveCount(0);
});

test('the expanded view can be left from its right-hand collapse control as well as the arrow', async ({ page }) => {
  await start(page);
  await panel(page).getByRole('button', { name: 'Expand tasks' }).click();
  await expect(page.getByRole('region', { name: 'Tasks' })).toBeVisible();
  await page.getByRole('button', { name: 'Collapse tasks' }).click();
  await expect(page.getByRole('region', { name: 'Tasks' })).toHaveCount(0);
  await expect(panel(page)).toBeVisible();
});

test('the task view header is the back arrow, the trail and the counts: no panel toggle (design 1c)', async ({ page }) => {
  await start(page);
  await panel(page).locator('.task-row-title').first().click();
  await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toBeVisible();
  await expect(toggle(page)).toHaveCount(0);
});

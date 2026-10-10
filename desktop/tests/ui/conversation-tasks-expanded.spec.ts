import { test, expect, type Page } from '@playwright/test';
import { writeFileSync } from 'node:fs';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { openApp, send, expectNoHorizontalOverflow } from './support/conversation';
import type { EngineQuestion, EngineTaskPage, EngineTaskRow } from '../../src/features/chat/engine-client';
import { expectAccessible, tokenColor } from './contracts';

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

for (const theme of ['light', 'dark'] as const) {
  test(`long task instructions are secondary Markdown inside a keyboard-scrollable bounded pane · ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    const rig = scenario();
    const longBrief = ['## Render the chart', '', 'Run `python plot.py` and **verify** the result.', '', ...Array.from({ length: 50 }, (_, i) => `- Check ${i + 1}: labels stay readable beside /a/very/long/path/${'segment'.repeat(20)}.`,), '', '```bash', 'printf "READY"', '```', '', 'Last instruction.'].join('\n');
    rig.taskPages!['1.1'] = pageOf(1, longBrief);
    await installMockEngine(page, rig);
    await openApp(page);
    await send(page, 'Ship it');
    await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
    await page.getByRole('complementary', { name: 'Tasks' }).getByRole('button', { name: 'Expand tasks' }).click();
    await rowButton(page, '1.1').click();
    const pane = detail(page, 'Read the current screen');
    const instructions = pane.getByRole('region', { name: 'Task instructions', exact: true });
    await expect(instructions.getByRole('heading', { name: 'Render the chart' })).toBeVisible();
    await expect(instructions.locator('li')).toHaveCount(50);
    await expect(instructions.locator('strong')).toHaveText('verify');
    await expect.poll(() => instructions.evaluate(el => {
      const s = getComputedStyle(el); const md = el.querySelector('.markdown')!;
      return { overflow: el.scrollHeight > el.clientHeight, bounded: el.clientHeight <= parseFloat(s.getPropertyValue('--task-detail-brief-max-height')), secondary: getComputedStyle(md).color === getComputedStyle(el.closest('.task-detail')!.querySelector('.task-detail-state-word')!).color, fits: el.getBoundingClientRect().bottom <= el.closest('.task-detail')!.getBoundingClientRect().bottom };
    })).toEqual({ overflow: true, bounded: true, secondary: true, fits: true });
    await instructions.focus();
    await page.keyboard.press('End');
    await expect.poll(() => instructions.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
    await expect(instructions.getByText('Last instruction.', { exact: true })).toBeVisible();
    await expect(pane.getByRole('button', { name: 'Open task' })).toBeVisible();
    await expectNoHorizontalOverflow(page);
    if (process.env.TASK_INSTRUCTION_SHOTS) await pane.screenshot({ path: `${process.env.TASK_INSTRUCTION_SHOTS}/long-instructions-${theme}-${test.info().project.name}.png` });
    await page.setViewportSize({ width: 1200, height: 480 });
    await expect.poll(() => pane.evaluate(el => el.getBoundingClientRect().bottom <= innerHeight)).toBeTruthy();
    await expectNoHorizontalOverflow(page);
  });
}

for (const scheme of ['light', 'dark'] as const) {
  test(`${scheme}: task detail heading retains the design 1d typography over shared title defaults`, async ({ page }, testInfo) => {
    await page.emulateMedia({ colorScheme: scheme });
    await openExpanded(page);
    await rowButton(page, '1.1').click();
    const pane = detail(page, 'Read the current screen');
    const heading = pane.getByRole('heading', { name: 'Read the current screen' });
    await expect(heading).toBeVisible();
    const typography = await heading.evaluate((node) => {
      const css = getComputedStyle(node);
      return { size: css.fontSize, weight: css.fontWeight, leading: css.lineHeight, tracking: css.letterSpacing, margin: css.margin, family: css.fontFamily };
    });
    const roles = await pane.evaluate((node) => Object.fromEntries(['.task-detail-crumb', '.task-detail-facts', '.task-detail-label', '.task-detail-text', '.task-detail-text p'].map((selector) => {
      const css = getComputedStyle(node.querySelector(selector)!);
      return [selector, { size: css.fontSize, weight: css.fontWeight, leading: css.lineHeight, color: css.color }];
    })));
    if (process.env.CODEAF_UI_RESULTS) {
      writeFileSync(`${process.env.CODEAF_UI_RESULTS}/task-title-${testInfo.project.name}-${scheme}.json`, JSON.stringify({ typography, roles }, null, 2));
      await pane.screenshot({ path: `${process.env.CODEAF_UI_RESULTS}/task-title-${testInfo.project.name}-${scheme}.png` });
    }
    expect(typography.size).toBe('18px');
    expect(typography.weight).toBe('600');
    expect(parseFloat(typography.leading)).toBeCloseTo(23.4, 2);
    expect(parseFloat(typography.tracking)).toBeCloseTo(-0.216, 3);
    expect(typography.margin).toBe('0px');
    expect(roles['.task-detail-crumb'].size).toBe('12px');
    expect(parseFloat(roles['.task-detail-crumb'].leading)).toBeCloseTo(17.4, 2);
    expect(roles['.task-detail-crumb'].color).toBe(await tokenColor(page, 'ink-3'));
    expect(roles['.task-detail-facts'].size).toBe('12px');
    expect(roles['.task-detail-label'].size).toBe('11px');
    expect(roles['.task-detail-label'].weight).toBe('500');
    for (const selector of ['.task-detail-text', '.task-detail-text p']) {
      expect(roles[selector].size).toBe('13px');
      expect(parseFloat(roles[selector].leading)).toBeCloseTo(20.8, 2);
      expect(roles[selector].color).toBe(await tokenColor(page, 'ink-2'));
    }
    await expect(pane).toHaveCSS('padding', '24px');
    await expect(pane).toHaveCSS('width', '428px');
  });
}

for (const theme of ['light', 'dark'] as const) {
 test(`task header counts open the matching filter and retain it through history, tabs and reload · ${theme}`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: theme });
  await installMockEngine(page, scenario());
  await openApp(page); await send(page, 'Ship it');
  const chatId = await page.locator('.workspace-tabstrip [role=tab][aria-selected=true]').getAttribute('id');
  const chat = page.locator(`#${chatId}`);
  const counts = page.locator('.conversation-bar-counts');
  const count = counts.getByRole('button', { name: '1 need you', exact: true });
  await expect(count).toHaveCSS('font-size', await counts.evaluate(node => getComputedStyle(node).fontSize));
  await expect(count).toHaveCSS('color', await counts.evaluate(node => getComputedStyle(node).color));
  await expect(count).toHaveCSS('padding', '0px');
  await expect(count).toHaveCSS('min-height', '0px');
  if (process.env.TASK_COUNT_SHOTS) await page.screenshot({ path: `${process.env.TASK_COUNT_SHOTS}/${test.info().project.name}-${theme}-header.png` });
  await counts.getByRole('button', { name: '1 need you', exact: true }).click();
  await expect(filterTab(page, /^Needs you/)).toHaveAttribute('aria-pressed', 'true');
  await expect(rowButton(page, '1.3')).toBeVisible();
  await expect(rowButton(page, '1.2')).toHaveCount(0);
  if (process.env.TASK_COUNT_SHOTS) await page.screenshot({ path: `${process.env.TASK_COUNT_SHOTS}/${test.info().project.name}-${theme}-needs.png` });
  const primary = await page.evaluate(() => /Mac/.test(navigator.platform) ? 'Meta' : 'Control');
  await page.keyboard.press(`${primary}+[`);
  await expect(table(page)).toHaveCount(0);
  await page.keyboard.press(`${primary}+]`);
  await expect(filterTab(page, /^Needs you/)).toHaveAttribute('aria-pressed', 'true');
  await table(page).getByRole('button', { name: 'Back to the conversation' }).click();
  await counts.getByRole('button', { name: '2 running', exact: true }).click();
  await expect(filterTab(page, /^Running/)).toHaveAttribute('aria-pressed', 'true');
  await expect(rowButton(page, '1.2')).toBeVisible();
  await expect(rowButton(page, '1.3')).toHaveCount(0);
  if (process.env.TASK_COUNT_SHOTS) await page.screenshot({ path: `${process.env.TASK_COUNT_SHOTS}/${test.info().project.name}-${theme}-running.png` });
  await filterTab(page, /^Done/).click();
  await page.getByRole('button', { name: 'New tab', exact: true }).click();
  await chat.click();
  await expect(filterTab(page, /^Done/)).toHaveAttribute('aria-pressed', 'true');
  await page.reload();
  await expect(filterTab(page, /^Done/)).toHaveAttribute('aria-pressed', 'true');
  await expect(rowButton(page, '1.1')).toBeVisible();
  if (process.env.TASK_COUNT_SHOTS) await page.screenshot({ path: `${process.env.TASK_COUNT_SHOTS}/${test.info().project.name}-${theme}-reloaded.png` });
  await expectAccessible(page);
 });
}

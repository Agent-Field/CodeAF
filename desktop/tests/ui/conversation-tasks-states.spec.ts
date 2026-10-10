import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { taskRows, withTasks, withTaskTree } from './support/scenarios';
import { openApp, posts, send } from './support/conversation';
import { expectAccessible, tokenColor } from './contracts';
import type { EngineTaskPage, EngineTaskRow } from '../../src/features/chat/engine-client';

// CV-100, CV-103, CV-106, CV-107, CV-111, CV-112, CV-115, CV-116, CV-119, CV-120, CV-122, CV-125, CV-126.
// The assertions name the design's own strings and measured geometry, so deleting the behaviour fails here.

const at = '2026-10-09T10:00:00Z';
const panel = (page: Page) => page.getByRole('complementary', { name: 'Tasks' });
const toggle = (page: Page) => page.getByRole('button', { name: /^Tasks · / });
const row = (page: Page, id: string) => panel(page).locator('.task-row', { has: page.locator(`[data-task-id="${id}"]`) });
const notice = (page: Page, title: string) => page.locator('.turn-v2 .task-notice').filter({ hasText: title });
const table = (page: Page) => page.getByRole('region', { name: 'Tasks' });
const tableRow = (page: Page, id: string) => table(page).locator(`.tasks-table-row[data-task-id="${id}"]`);

const WIDTHS = [320, 600, 1200] as const;

async function paint(page: Page, token: string, prop: 'color' | 'backgroundColor' = 'backgroundColor') {
  return page.evaluate(([name, property]) => {
    const probe = document.createElement('span');
    probe.style[property === 'color' ? 'color' : 'background'] = `var(--${name})`;
    document.body.append(probe);
    const value = getComputedStyle(probe)[property];
    probe.remove();
    return value;
  }, [token, prop] as const);
}

async function shadow(page: Page, token: string) {
  return page.evaluate(name => {
    const probe = document.createElement('div');
    probe.style.boxShadow = `var(--${name})`;
    document.body.append(probe);
    const value = getComputedStyle(probe).boxShadow;
    probe.remove();
    return value;
  }, token);
}

async function boot(page: Page, theme: 'light' | 'dark', scenario: Scenario, size = { width: 1200, height: 800 }, prompt = 'Migrate the settings screen') {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'no-preference' });
  await page.setViewportSize(size);
  const engine = await installMockEngine(page, scenario);
  await openApp(page);
  await send(page, prompt);
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
  return engine;
}

function noticesScenario(): Scenario {
  const base = withTasks();
  const tasks = taskRows.map(row => (row.ID === '2.3' ? { ...row, Status: 'paused' } : { ...row }));
  const running = tasks.find(row => row.ID === '2.2')!;
  const live = { ...running, Model: 'deepseek/deepseek-v4.1-flash', USD: 0.06, Steps: 6, Live: { Step: 7, Command: 'go test ./internal/parse/...', Since: at } };
  const planned = tasks.map(row => (row.ID === '2.2' ? live : row));
  const done = planned.find(row => row.ID === '2.1')!;
  const pageOf = (row: EngineTaskRow, extra: Partial<EngineTaskPage> = {}): EngineTaskPage => ({
    Row: row, Description: 'Read the screen.', Result: '', Checks: [], Steps: [], Notes: [], Children: [], Folder: '/mock-workspace', ...extra,
  });
  const asides = [
    { Role: 'aside' as const, Text: 'Read the current screen done.', TaskIDs: ['2.1'] },
    { Role: 'aside' as const, Text: 'Port the form fields running.', TaskIDs: ['2.2'] },
    { Role: 'aside' as const, Text: 'Update the snapshot tests paused.', TaskIDs: ['2.3'] },
  ];
  return {
    initial: { title: base.initial.title, entries: [] },
    turns: [{ entries: [...asides, { Role: 'assistant', Text: 'I split the work into three parts.', Answer: true }], patch: { tasks: planned } }],
    taskPages: {
      '2.1': pageOf(done, {
        Steps: [
          { step: 1, command: 'ls src', observation: 'listed' },
          { step: 2, command: 'rm -rf testdata/old', refused: true, not_run: true },
        ],
      }),
      '2.2': pageOf(live, {
        Description: 'Port the fields onto the shared primitives.',
        Steps: [
          { step: 1, command: 'rg -n Comma testdata', observation: 'found' },
          { step: 2, command: 'rm -rf testdata/old', refused: true, not_run: true },
        ],
        Live: live.Live,
        Notes: [{ Author: 'worker-1', Body: 'The nested fixture had a second bug, a missing closing brace. I fixed it too.' }],
        WaitRows: [{ ID: '2.1', Title: 'Read the current screen', Status: 'done' }],
      }),
    },
  };
}

function treeScenario(): Scenario {
  const base = withTaskTree();
  const tasks = (base.initial.tasks ?? []).map(row => {
    if (row.ID === '4.3') return { ...row, Paused: true };
    if (row.ID === '4.1') return { ...row, Model: 'deepseek/deepseek-v4.1-flash', USD: 0.14 };
    return { ...row };
  });
  const questions = (base.initial.questions ?? []).map(question => ({ ...question, head: 'Keep strict? Picks Suggested in 12s' }));
  const answer = (base.initial.entries ?? []).find(entry => entry.Role === 'assistant');
  const aside = (base.initial.entries ?? []).find(entry => entry.Role === 'aside');
  const lines = Array.from({ length: 70 }, (_, index) => `Scroll line ${index + 1} keeps the column taller than the pane.`);
  return {
    initial: { title: base.initial.title, entries: [] },
    turns: [{
      entries: [aside!, { ...answer!, Text: `${answer!.Text}\n${lines.join('\n')}` }],
      patch: { tasks, questions, needsPerson: true },
    }],
    taskPages: base.taskPages,
  };
}

for (const theme of ['light', 'dark'] as const) {
  test(`CV-100 task notices: done, running with a live step, your call · ${theme}`, async ({ page }) => {
    await boot(page, theme, noticesScenario(), { width: 400, height: 800 });
    const done = notice(page, 'Read the current screen');
    const running = notice(page, 'Port the form fields');
    const call = notice(page, 'Update the snapshot tests');
    await expect(done).toBeVisible();
    await expect(done).not.toHaveAttribute('data-live', 'true');
    await expect(done.getByRole('img', { name: 'Done' })).toBeVisible();
    await expect(done).toHaveCSS('box-shadow', await shadow(page, 'sh-1'));
    await expect(done.locator('.task-notice-live')).toHaveCount(0);

    await expect(running).toHaveAttribute('data-live', 'true');
    await expect(running.getByRole('img', { name: 'Running' })).toBeVisible();
    await expect(running).toHaveCSS('box-shadow', await shadow(page, 'sh-2'));
    const live = running.locator('.task-notice-live');
    await expect(live).toHaveText('go test ./internal/parse/...');
    await expect(live).toHaveCSS('font-family', await page.evaluate(() => {
      const probe = document.createElement('span');
      probe.style.fontFamily = 'var(--mono)';
      document.body.append(probe);
      const family = getComputedStyle(probe).fontFamily;
      probe.remove();
      return family;
    }));
    await expect(running.locator('.task-notice-chevron')).toHaveCSS('opacity', '1');

    await expect(call).not.toHaveAttribute('data-live', 'true');
    await expect(call.getByRole('img', { name: 'Your call' })).toBeVisible();
    await expect(call.locator('.status-mark-dot')).toHaveCSS('background-color', await paint(page, 'amber'));
    await expect(call.locator('.status-mark-dot')).toHaveCSS('width', '6px');
    await expect(call.locator('.task-notice-title')).toHaveCSS('color', await tokenColor(page, 'ink'));

    for (const width of [320, 600, 1200] as const) {
      await page.setViewportSize({ width, height: 800 });
      await expect(running).toBeVisible();
      expect(await running.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    }
  });

  test(`CV-103 notice and row menus: Open in new tab, Pause, Resume, Stop · ${theme}`, async ({ page }) => {
    test.setTimeout(60_000);
    const engine = await boot(page, theme, noticesScenario());
    const running = notice(page, 'Port the form fields');
    await running.click({ button: 'right' });
    const menu = page.getByRole('menu', { name: 'Port the form fields actions' });
    await expect(menu.getByRole('menuitem', { name: 'Open in new tab' })).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Pause' })).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Stop' })).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: 'Resume' })).toHaveCount(0);
    await page.keyboard.press('Escape');

    await running.locator('.task-notice-row').focus();
    await page.keyboard.press('Shift+F10');
    await expect(menu.getByRole('menuitem', { name: 'Open in new tab' })).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(menu.getByRole('menuitem', { name: 'Pause' })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect.poll(() => posts(engine, '/tasks/2.2/pause').length).toBe(1);

    const yours = notice(page, 'Update the snapshot tests');
    await yours.click({ button: 'right' });
    const resume = page.getByRole('menu', { name: 'Update the snapshot tests actions' });
    await expect(resume.getByRole('menuitem', { name: 'Resume' })).toBeVisible();
    await expect(resume.getByRole('menuitem', { name: 'Pause' })).toHaveCount(0);
    await expect(resume.getByRole('menuitem', { name: 'Stop' })).toBeVisible();
    await page.keyboard.press('Escape');

    const finished = notice(page, 'Read the current screen');
    await finished.click({ button: 'right' });
    const doneMenu = page.getByRole('menu', { name: 'Read the current screen actions' });
    await expect(doneMenu.getByRole('menuitem', { name: 'Open in new tab' })).toBeVisible();
    await expect(doneMenu.getByRole('menuitem', { name: 'Pause' })).toHaveCount(0);
    await expect(doneMenu.getByRole('menuitem', { name: 'Stop' })).toHaveCount(0);
    await page.keyboard.press('Escape');

    const task = row(page, '2.2');
    await task.locator('.task-row-title').click({ button: 'right' });
    const rowMenu = page.getByRole('menu', { name: 'Port the form fields actions' });
    await expect(rowMenu.getByRole('menuitem', { name: 'Open in new tab' })).toBeVisible();
    await expect(rowMenu.getByRole('menuitem', { name: 'Pause' })).toBeVisible();
    await expect(rowMenu.getByRole('menuitem', { name: 'Stop' })).toBeVisible();
    await page.keyboard.press('Escape');
    await task.locator('.task-panel-main').focus();
    await page.keyboard.press('Shift+F10');
    const rowMenuKeys = page.getByRole('menu', { name: 'Port the form fields actions' });
    await expect(rowMenuKeys.getByRole('menuitem', { name: 'Open in new tab' })).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(rowMenuKeys.getByRole('menuitem', { name: 'Pause' })).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(rowMenuKeys.getByRole('menuitem', { name: 'Stop' })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect.poll(() => posts(engine, '/tasks/2.2/cancel').length).toBe(1);

    engine.update({ tasks: taskRows.map(item => (item.ID === '2.2' ? { ...item, Status: 'paused', Paused: true } : item)) });
    await expect(task.locator('.status-mark')).toHaveAttribute('data-status', 'paused');
    await task.locator('.task-row-title').click({ button: 'right' });
    await expect(page.getByRole('menuitem', { name: 'Resume' })).toBeVisible();
    await expect(page.getByRole('menuitem', { name: 'Pause' })).toHaveCount(0);
  });

  test(`CV-106 CV-107 progress strip and tree glyphs · ${theme}`, async ({ page }) => {
    await boot(page, theme, treeScenario(), { width: 1200, height: 800 }, 'Ship trailing-comma support');
    const strip = panel(page).locator('.task-progress');
    await expect(strip).toHaveAttribute('aria-label', '2 done, 3 running, 2 waiting, 1 failed, 5 queued');
    await expect(strip).toHaveCSS('height', '3px');
    const segments = await strip.locator('.task-progress-cell').evaluateAll(cells => cells.map(cell => cell.getAttribute('data-segment')));
    expect(segments).toEqual([
      ...Array(2).fill('done'),
      ...Array(3).fill('running'),
      ...Array(2).fill('waiting'),
      'failed',
      ...Array(5).fill('queued'),
    ]);
    await expect(strip.locator('[data-segment="done"]').first()).toHaveCSS('background-color', await paint(page, 'ink-3'));
    await expect(strip.locator('[data-segment="running"]').first()).toHaveCSS('background-color', await paint(page, 'accent'));
    await expect(strip.locator('[data-segment="waiting"]').first()).toHaveCSS('background-color', await paint(page, 'amber'));
    await expect(strip.locator('[data-segment="failed"]').first()).toHaveCSS('background-color', await paint(page, 'danger'));
    await expect(strip.locator('[data-segment="queued"]').first()).toHaveCSS('background-color', await paint(page, 'field-2'));

    const ship = row(page, '3');
    await expect(ship.getByRole('button', { name: 'Collapse Ship trailing-comma support' })).toBeVisible();
    await expect(ship.locator('.task-row-meta')).toHaveText('1/6');
    const guide = panel(page).locator('.task-tree[data-depth="1"]').first();
    // Used border widths round a half pixel up to 1px, so the proof is the specified token, matched against a probe.
    const guideRule = await guide.evaluate(el => {
      const probe = document.createElement('span');
      probe.style.borderInlineStart = 'var(--hairline) solid var(--line)';
      document.body.append(probe);
      const width = getComputedStyle(probe).borderInlineStartWidth;
      const color = getComputedStyle(probe).borderInlineStartColor;
      probe.remove();
      let specified = '';
      for (const sheet of document.styleSheets) {
        let rules: CSSRuleList;
        try { rules = sheet.cssRules; } catch { continue; }
        for (const rule of rules) {
          if (rule instanceof CSSStyleRule && rule.selectorText.includes('.task-tree:not([data-depth="0"])')) specified = rule.style.getPropertyValue('border-inline-start');
        }
      }
      return { specified, width, color, used: getComputedStyle(el).borderInlineStartWidth, usedColor: getComputedStyle(el).borderInlineStartColor };
    });
    expect(guideRule.specified).toContain('var(--hairline)');
    expect(guideRule.used).toBe(guideRule.width);
    expect(guideRule.usedColor).toBe(guideRule.color);

    const settled = row(page, '3.3');
    await expect(settled).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await settled.locator('.task-panel-main').click();
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.mouse.move(0, 0);
    await expect(settled).toHaveAttribute('data-current', 'true');
    await expect(settled).toHaveCSS('background-color', await paint(page, 'field'));

    const runningDot = row(page, '3.1').locator('.status-mark-dot');
    await expect(runningDot).toHaveCSS('width', '6px');
    await expect(runningDot).toHaveCSS('height', '6px');
    await expect(runningDot).toHaveCSS('background-color', await paint(page, 'accent'));

    const waitingDot = row(page, '3.2').locator('.status-mark-dot');
    await expect(row(page, '3.2').locator('.status-mark')).toHaveAttribute('data-status', 'waiting');
    await expect(waitingDot).toHaveCSS('background-color', await paint(page, 'amber'));

    const queued = row(page, '4.5').locator('.status-mark');
    await queued.scrollIntoViewIfNeeded();
    await expect(queued).toHaveAttribute('data-status', 'queued');
    const ring = queued.locator('.status-mark-dot');
    await expect(ring).toHaveCSS('width', '6px');
    await expect(ring).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await expect(ring).toHaveCSS('box-shadow', /inset/);

    const failed = row(page, '4.2').locator('.status-mark');
    await failed.scrollIntoViewIfNeeded();
    await expect(failed).toHaveAttribute('data-status', 'incomplete');
    await expect(failed.locator('.status-mark-dot')).toHaveCSS('width', '6px');
    await expect(failed.locator('.status-mark-dot')).toHaveCSS('background-color', await paint(page, 'danger'));

    const paused = row(page, '4.3').locator('.status-mark');
    await paused.scrollIntoViewIfNeeded();
    await expect(paused).toHaveAttribute('data-status', 'paused');
    await expect(paused).toHaveAttribute('aria-label', 'Paused');
    await expect(paused.locator('.status-mark-dot')).toHaveCount(0);

    const waited = row(page, '4.5').locator('.task-row-wait');
    await expect(waited).toHaveText('Split by loader /');
    await expect(waited).toHaveCSS('opacity', '0.7');
  });

  test(`CV-111 CV-112 the column is instant; a narrow sheet slides, fades, and locks the scroll · ${theme}`, async ({ page }) => {
    test.setTimeout(60_000);
    await boot(page, theme, treeScenario(), { width: 1200, height: 700 }, 'Ship trailing-comma support');
    await expect(panel(page)).toHaveAttribute('data-variant', 'column');
    await expect(panel(page)).toHaveCSS('animation-name', 'none');
    await expect(page.locator('.task-panel-backdrop')).toHaveCount(0);
    await expect(page.locator('.conversation-main')).not.toHaveAttribute('data-sheet', 'true');
    await expect(page.locator('.conversation-scroll')).toHaveCSS('overflow-y', 'auto');

    for (const width of [600, 320] as const) {
      await page.setViewportSize({ width, height: 700 });
      await expect(panel(page)).toHaveCount(0);
      await toggle(page).click();
      const sheet = panel(page);
      await expect(sheet).toHaveAttribute('data-variant', 'sheet');
      await expect(sheet).toHaveCSS('animation-name', 'task-sheet-enter');
      await expect(sheet).toHaveCSS('animation-duration', '0.32s');
      const backdrop = page.locator('.task-panel-backdrop');
      await expect(backdrop).toBeVisible();
      await expect(backdrop).toHaveCSS('animation-name', 'task-backdrop-enter');
      const motion = await page.evaluate(() => {
        const read = (name: string) => {
          for (const styleSheet of document.styleSheets) {
            let rules: CSSRuleList;
            try { rules = styleSheet.cssRules; } catch { continue; }
            for (const rule of rules) {
              if (rule instanceof CSSKeyframesRule && rule.name === name) return [...rule.cssRules].map(item => item.cssText).join(' ');
            }
          }
          return '';
        };
        return { sheet: read('task-sheet-enter'), fade: read('task-backdrop-enter') };
      });
      expect(motion.sheet).toContain('translateX(100%)');
      expect(motion.fade).toMatch(/opacity:\s*(0|var\(--opacity-transparent\))/);
      await expect(page.locator('.conversation-main')).toHaveAttribute('data-sheet', 'true');
      const scroller = page.locator('.conversation-scroll');
      await expect(scroller).toHaveCSS('overflow-y', 'hidden');
      const room = await scroller.evaluate(el => el.scrollHeight - el.clientHeight);
      expect(room).toBeGreaterThan(80);
      await scroller.evaluate(el => { el.scrollTop = 80; });
      await page.mouse.move(24, 180);
      await page.mouse.wheel(0, 400);
      await expect.poll(() => scroller.evaluate(el => el.scrollTop)).toBe(80);
      await page.keyboard.press('Escape');
      await expect(panel(page)).toHaveCount(0);
      await expect(scroller).toHaveCSS('overflow-y', 'auto');
      await scroller.evaluate(el => { el.scrollTop = 40; });
      await scroller.hover();
      await page.mouse.wheel(0, 240);
      await expect.poll(() => scroller.evaluate(el => el.scrollTop)).toBeGreaterThan(40);
    }

    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await toggle(page).click();
    await expect(panel(page)).toHaveCSS('animation-name', 'none');
    await expect(page.locator('.task-panel-backdrop')).toHaveCSS('animation-name', 'none');
  });

  test(`CV-115 CV-116 search, countdown, waits, and the 48px mask · ${theme}`, async ({ page }) => {
    test.setTimeout(60_000);
    await boot(page, theme, treeScenario(), { width: 1200, height: 560 }, 'Ship trailing-comma support');
    await panel(page).getByRole('button', { name: 'Expand tasks' }).click();
    const live = tableRow(page, '3.1');
    await expect(live.locator('.tasks-table-command')).toHaveText('go test ./internal/parse/...');
    await expect(live.locator('.tasks-table-state')).toHaveText('Running');
    await expect(live.locator('.tasks-table-age')).not.toHaveText('');

    const needs = tableRow(page, '3.2');
    await expect(needs.locator('.tasks-table-detail')).toContainText('Keep strict? Picks Suggested in 12s');
    await expect(needs.locator('.tasks-table-state')).toHaveText('Needs you');

    const waited = tableRow(page, '4.5');
    await expect(waited.locator('.tasks-table-detail')).toHaveText('Waits on Split by loader');
    await expect(waited.locator('.tasks-table-state')).toHaveText('Queued');

    const done = tableRow(page, '4.1');
    await expect(done.locator('.tasks-table-detail')).toHaveText('deepseek-v4.1-flash · 5 steps · $0.14');
    await expect(done.locator('.tasks-table-state')).toHaveText('Done');
    await expect(done.locator('.tasks-table-age')).toHaveText('');

    const scroll = table(page).locator('.tasks-table-scroll');
    await expect(scroll).toHaveAttribute('data-more', 'true');
    const mask = await scroll.evaluate(el => {
      const style = getComputedStyle(el);
      return `${style.maskImage} ${(style as CSSStyleDeclaration & { webkitMaskImage?: string }).webkitMaskImage ?? ''}`;
    });
    expect(mask).toMatch(/48px/);
    const head = await table(page).locator('.tasks-table-head').boundingBox();
    await scroll.evaluate(el => { el.scrollTop = el.scrollHeight; });
    await expect(scroll).not.toHaveAttribute('data-more', 'true');
    expect((await table(page).locator('.tasks-table-head').boundingBox())!.y).toBe(head!.y);

    await page.getByRole('button', { name: 'Search tasks' }).focus();
    await page.keyboard.press('Enter');
    const field = page.getByRole('textbox', { name: 'Search tasks' });
    await expect(field).toBeFocused();
    await field.scrollIntoViewIfNeeded();
    await field.fill('Convert env');
    await expect(table(page).locator('.tasks-table-group[data-task-id="4"]')).toBeVisible();
    await expect(tableRow(page, '4.2')).toBeVisible();
    await expect(tableRow(page, '4.5')).toBeVisible();
    await expect(table(page).locator('.tasks-table-group[data-task-id="3"]')).toHaveCount(0);
    await expect(tableRow(page, '4.4')).toHaveCount(0);
    await page.keyboard.press('Escape');
    await expect(field).toHaveCount(0);
    await expect(table(page).locator('.tasks-table-group[data-task-id="3"]')).toBeVisible();
    await expectAccessible(page);

    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 640 });
      expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= window.innerWidth)).toBe(true);
      // The detail pane keeps its width down to the pane, so the search control is reachable beside it from 600 up.
      if (width >= 600) await expect(table(page).getByRole('button', { name: 'Search tasks' })).toBeVisible();
      await expect(tableRow(page, '3.2').locator('.tasks-table-detail')).toContainText('Keep strict? Picks Suggested in 12s');
    }
  });

  test(`CV-119 CV-120 CV-122 CV-125 CV-126 task view status, pause, stop, log, note, waits · ${theme}`, async ({ page }) => {
    test.setTimeout(90_000);
    const engine = await boot(page, theme, noticesScenario());
    await row(page, '2.2').locator('.task-panel-main').click();
    const title = page.getByRole('heading', { name: 'Port the form fields' });
    await expect(title).toBeVisible();
    await expect(title).toHaveCSS('font-size', '22px');
    const word = page.locator('.task-view-state-word');
    await expect(word).toHaveText('Running');
    await expect(word).toHaveCSS('color', await tokenColor(page, 'ink-2'));
    await expect(page.locator('.task-view-state-parts')).toHaveText(/^step 7 · \d+h \d+m · Flash · \$0\.06$/);
    await expect(page.locator('.task-view-dot')).toHaveCSS('width', '6px');
    await expect(page.locator('.task-view-dot')).toHaveCSS('background-color', await paint(page, 'accent'));

    const view = page.locator('.task-view');
    await view.getByRole('button', { name: 'Pause', exact: true }).focus();
    await page.keyboard.press('Enter');
    await expect.poll(() => posts(engine, '/tasks/2.2/pause').length).toBe(1);
    await view.getByRole('button', { name: 'More task actions' }).focus();
    await page.keyboard.press('Enter');
    const stop = page.getByRole('menuitem', { name: 'Stop' });
    const opened = await stop.waitFor({ state: 'visible', timeout: 1000 }).then(() => true, () => false);
    if (opened) {
      await stop.focus();
      await page.keyboard.press('Enter');
    }
    await expect.poll(() => posts(engine, '/tasks/2.2/cancel').length).toBe(1);

    const log = page.getByRole('region', { name: 'Work log' });
    await expect(log.getByRole('button', { name: 'Work log · 3 commands · 1 refused' })).toHaveAttribute('aria-expanded', 'true');
    const names = await log.locator('.task-log-text').evaluateAll(nodes => nodes.map(node => getComputedStyle(node).animationName));
    expect(names.filter(name => name.includes('shimmer'))).toEqual(['task-log-shimmer']);
    await expect(log.locator('.task-log-step[data-state="running"] .task-log-text')).toHaveText('go test ./internal/parse/...');
    await expect(log.locator('.task-log-list')).toHaveCSS('font-family', await page.evaluate(() => {
      const probe = document.createElement('span');
      probe.style.fontFamily = 'var(--font-mono)';
      document.body.append(probe);
      const family = getComputedStyle(probe).fontFamily;
      probe.remove();
      return family;
    }));

    const worker = page.locator('.task-note-other').filter({ hasText: 'missing closing brace' });
    await expect(worker.locator('.task-note-author')).toHaveText('Note from worker');
    await expect(worker).toHaveCSS('max-width', '85%');

    const waits = page.getByRole('region', { name: 'Waits on' });
    await expect(waits.getByRole('heading', { name: 'Waits on' })).toBeVisible();
    await waits.getByRole('button', { name: /Read the current screen/ }).focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { name: 'Read the current screen' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Waits on' })).toHaveCount(0);
    const folded = page.getByRole('region', { name: 'Work log' });
    await expect(folded.getByRole('button', { name: 'Work log · 2 commands · 1 refused' })).toHaveAttribute('aria-expanded', 'false');
    await expect(folded.locator('.task-log-list')).toHaveCount(0);
    await folded.getByRole('button', { name: 'Work log · 2 commands · 1 refused' }).click();
    await expect(folded.locator('.task-log-outcome[data-state="refused"]')).toHaveText('refused');
    await expect(folded.locator('.task-log-step[data-state="running"]')).toHaveCount(0);

    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 700 });
      await expect(page.getByRole('heading', { name: 'Read the current screen' })).toBeVisible();
      await expect(page.getByRole('button', { name: 'Work log · 2 commands · 1 refused' })).toBeVisible();
      expect(await page.locator('.task-view').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    }
    await expectAccessible(page);
  });
}

import { test, expect, type Locator, type Page, type Route } from '@playwright/test';
import design from '../../src/design/tokens.json' with { type: 'json' };
import type { WorldRecord, WorldRow } from '../../src/features/world/types';
import { expectAccessible, tokenColorIn } from './contracts';
import { expectNoHorizontalOverflow } from './support/conversation';
import { installMockEngine } from './support/mock-engine';

// SH-058/059/060/061/102 use engine records, so a specimen-only state cannot satisfy these contracts.
async function openWorkspace(page: Page) {
  await page.clock.install();
  const tabs = ['Reading', 'Background', 'Member', 'Other'].map((title, index) => ({
    id: `state-${index}`, kind: 'conversation', title, titleSource: 'manual', pinned: false, draft: '',
    sessionFile: `/workspace/state-${index}/session.jsonl`, ...(index > 1 ? { groupId: 'release' } : {}),
  }));
  await page.addInitScript(state => {
    localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state));
  }, { tabs, groups: [{ id: 'release', title: 'Release', collapsed: true }], activeId: tabs[0].id,
    closed: [], nextNumber: 5, recentIds: tabs.map(tab => tab.id) });
  const engine = await installMockEngine(page, { initial: {
    sessionFile: tabs[0].sessionFile,
    entries: [{ Role: 'user', Text: 'Keep reading this conversation.' }],
  } });
  const rows: WorldRow[] = tabs.map(tab => ({
    chatId: tab.id, sessionFile: tab.sessionFile, title: tab.title, running: false,
    needsYou: 0, failed: 0, tasksRunning: 0, tasksTotal: 0, attached: false, archived: false,
  }));
  // The tab strip and the notification path each hold their own world stream, so a record is delivered to every waiting one.
  let held: Route[] = [];
  let seq = 1;
  let acknowledged = 0;
  const fulfill = (route: Route, record: WorldRecord) => route.fulfill({
    contentType: 'text/event-stream', body: `id: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n`,
  });
  await page.route('**/api/engine/events?*', async route => {
    const after = Number(new URL(route.request().url()).searchParams.get('after'));
    acknowledged = Math.max(acknowledged, after);
    // A stream that connects late, or reconnects behind the feed, is told the whole present, as the engine does for a cursor it cannot replay.
    if (after < seq) await fulfill(route, { epoch: 'tab-state', seq, type: 'reset', at: '2026-10-10T12:00:00Z', payload: { rows, items: [] } });
    else held.push(route);
  });
  await page.goto('/');
  await expect(page.getByText('Keep reading this conversation.', { exact: true })).toBeVisible();
  await page.mouse.move(0, 799);
  const publish = async (index: number, patch: Partial<WorldRow>) => {
    // Advancing the reconnect clock keeps the stream test independent of wall-clock sleeps.
    await page.clock.runFor(1100);
    await expect.poll(() => held.length > 0).toBe(true);
    rows[index] = { ...rows[index], ...patch };
    const routes = held;
    held = [];
    const record: WorldRecord = { epoch: 'tab-state', seq: ++seq, type: 'world', at: '2026-10-10T12:00:01Z', payload: { rows: [rows[index]], removed: [] } };
    for (const route of routes) await fulfill(route, record);
    await page.clock.runFor(1100);
    await expect.poll(() => acknowledged).toBe(seq);
  };
  return { publish, engine, tabs };
}

async function expectDot(owner: Locator, label: 'Needs you' | 'Failed') {
  const dot = owner.getByRole('img', { name: label, exact: true });
  await expect(dot).toHaveCount(1);
  await expect(dot).toHaveAttribute('data-state', label === 'Needs you' ? 'waiting' : 'failed');
  await expect(dot).toHaveCSS('background-color', await tokenColorIn(owner, label === 'Needs you' ? 'amber' : 'danger'));
  const size = await dot.evaluate(element => {
    const rect = element.getBoundingClientRect();
    return { width: rect.width, height: rect.height };
  });
  expect(size).toEqual({ width: 6, height: 6 });
}

for (const scheme of ['light', 'dark'] as const) {
  for (const width of [320, 600, 1200]) {
    test(`SH-058–061 live inactive tab states · ${scheme} · ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const { publish, engine, tabs } = await openWorkspace(page);
      const reading = page.getByRole('tab', { name: 'Reading', exact: true });
      const background = page.getByRole('tab', { name: 'Background', exact: true });
      const chip = page.locator('.workspace-tab', { has: background });
      const title = background.locator('.workspace-tab-title');
      // Shell 3j: at the small breakpoint an inactive tab is the 44px chip and does not draw a title.
      // The ink lift is that title's colour, so it is measured only while the title is on screen.
      const titled = width > design.breakpoints.small;
      if (titled) await expect(title).toHaveCSS('color', await tokenColorIn(background, 'ink-2'));
      else {
        await expect(chip).toHaveAttribute('data-compressed', 'true');
        await expect(title).toHaveCount(0);
      }
      const quietMarkup = await background.innerHTML();

      await publish(1, { running: true, tasksRunning: 7, tasksTotal: 12 });
      // Counts and running are real engine inputs, but the tab stays the quiet kind glyph (and its title, when the chip draws one).
      await expect.poll(() => background.innerHTML()).toBe(quietMarkup);
      await expect(background.locator('.app-icon')).toHaveCount(1);
      await expect(chip.locator('.tab-dot, .tab-badge, [role="status"], [role="progressbar"]')).toHaveCount(0);
      await expect(background).not.toHaveAttribute('aria-description');
      if (titled) {
        await expect(background).toHaveText('Background');
        await expect(title).toHaveCSS('color', await tokenColorIn(background, 'ink-2'));
      }

      await publish(1, { needsYou: 1 });
      await expectDot(background, 'Needs you');
      await expect(background.locator('.app-icon')).toHaveCount(0);
      await expect(background).toHaveAttribute('aria-description', 'Needs you');
      if (titled) await expect(title).toHaveCSS('color', await tokenColorIn(background, 'ink'));
      await expect(background).toHaveAttribute('aria-selected', 'false');
      await expect(reading).toHaveAttribute('aria-selected', 'true');
      await page.clock.resume();
      await expectAccessible(page);
      await page.clock.pauseAt(await page.evaluate(() => Date.now() + 100));

      await publish(1, { running: false, needsYou: 0, failed: 1, tasksRunning: 0 });
      await expectDot(background, 'Failed');
      await expect(background.locator('.app-icon')).toHaveCount(0);
      await expect(background).toHaveAttribute('aria-description', 'Failed');
      await expect(background.getByRole('img', { name: 'Needs you' })).toHaveCount(0);
      if (titled) await expect(title).toHaveCSS('color', await tokenColorIn(background, 'ink-2'));
      await expect(background).toHaveAttribute('aria-selected', 'false');
      expect(engine.calls.filter(call => call.method === 'POST' && call.path === '/api/engine/sessions').map(call => call.body.sessionFile)).toEqual([tabs[0].sessionFile]);
      await expectNoHorizontalOverflow(page);
      await page.clock.resume();
      await expectAccessible(page);

      // Arrow navigation must reach the failed background tab even under narrow-width overflow.
      await reading.focus();
      await reading.press('ArrowRight');
      await expect(background).toBeFocused();
      await expect(background).toHaveAttribute('aria-selected', 'true');
      await expect.poll(() => engine.calls.filter(call => call.method === 'POST' && call.path === '/api/engine/sessions').map(call => call.body.sessionFile)).toEqual([tabs[0].sessionFile, tabs[1].sessionFile]);
    });

    test(`SH-102 live collapsed group attention · ${scheme} · ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const { publish } = await openWorkspace(page);
      const group = page.getByRole('button', { name: 'Release 2', exact: true });
      await expect(group).toHaveAttribute('aria-expanded', 'false');
      await expect(group.locator('.tab-dot')).toHaveCount(0);
      await publish(2, { running: true, needsYou: 1 });
      const waiting = page.getByRole('button', { name: 'Needs you Release 2', exact: true });
      await expectDot(waiting, 'Needs you');
      await expect(waiting).toHaveAttribute('aria-expanded', 'false');
      await expect(page.getByRole('tab', { name: 'Member', exact: true })).toHaveCount(0);
      await waiting.focus();
      await expect(waiting).toBeFocused();
      await page.clock.resume();
      await expectAccessible(page);
      await waiting.press('Enter');
      const member = page.getByRole('tab', { name: 'Member', exact: true });
      await expect(member).toBeVisible();
      await expectDot(member, 'Needs you');
      const expanded = page.getByRole('button', { name: 'Release', exact: true });
      await expect(expanded).toHaveAttribute('aria-expanded', 'true');
      await expect(expanded.locator('.tab-dot')).toHaveCount(0);
      await expanded.press('Space');
      await expect(waiting).toHaveAttribute('aria-expanded', 'false');
      await expectDot(waiting, 'Needs you');

      await page.clock.pauseAt(await page.evaluate(() => Date.now() + 100));
      await publish(2, { needsYou: 0 });
      await expect(group.locator('.tab-dot')).toHaveCount(0);
      await expect(group).toHaveAttribute('aria-expanded', 'false');
      await expectNoHorizontalOverflow(page);
      await page.clock.resume();
      await expectAccessible(page);
    });
  }
}

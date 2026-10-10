import { test, expect, type Route } from '@playwright/test';
import type { WorldRecord, WorldRow } from '../../src/features/world/types';
import { expectAccessible, tokenColorIn } from './contracts';
import { expectNoHorizontalOverflow } from './support/conversation';
import { installMockEngine } from './support/mock-engine';

// CV-301 exercises the window's real world reader, rather than injecting state into React.
for (const scheme of ['light', 'dark'] as const) {
  for (const width of [320, 600, 1200]) {
    test(`CV-301 background summaries follow world records · ${scheme} · ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      await page.clock.install();
      const tabs = ['Reading', 'Background', 'Quiet'].map((title, index) => ({
        id: `chat-${index}`, title, titleSource: 'manual', pinned: false, draft: '',
        sessionFile: `/workspace/chat-${index}/session.jsonl`,
      }));
      await page.addInitScript(state => {
        localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify(state));
      }, { tabs, groups: [], activeId: tabs[0].id, closed: [], nextNumber: 4, recentIds: tabs.map(tab => tab.id) });
      const engine = await installMockEngine(page, { initial: {
        sessionFile: tabs[0].sessionFile,
        entries: [{ Role: 'user', Text: 'Keep this conversation in front.' }],
      } });
      const rows: WorldRow[] = tabs.map(tab => ({
        chatId: tab.id, sessionFile: tab.sessionFile, title: tab.title,
        running: false, needsYou: 0, failed: 0, tasksRunning: 0, tasksTotal: 0,
        attached: false, archived: false,
      }));
      let pending: Route | undefined;
      let delivered = 0;
      const publish = async (route: Route, record: WorldRecord) => {
        await route.fulfill({ contentType: 'text/event-stream', body: `id: ${record.seq}\ndata: ${JSON.stringify(record)}\n\n` });
        delivered = record.seq;
      };
      await page.route('**/api/engine/events?*', async route => {
        const after = Number(new URL(route.request().url()).searchParams.get('after'));
        if (after === 0) {
          await publish(route, { epoch: 'background-test', seq: 1, type: 'reset', at: '2026-10-10T12:00:00Z', payload: { rows, items: [] } });
        } else {
          pending = route;
        }
      });
      const sessionReads = () => engine.calls.filter(call => call.method === 'GET' && /^\/api\/engine\/sessions\/[^/]+$/.test(call.path));
      const attachments = () => engine.calls.filter(call => call.method === 'POST' && call.path === '/api/engine/sessions');
      await page.goto('/');
      const reading = page.getByRole('tab', { name: 'Reading', exact: true });
      const background = page.getByRole('tab', { name: 'Background', exact: true });
      const quiet = page.getByRole('tab', { name: 'Quiet', exact: true });
      await expect(page.getByText('Keep this conversation in front.', { exact: true })).toBeVisible();
      await expect.poll(() => delivered).toBe(1);
      await expect(background.getByRole('img', { name: 'Needs you' })).toHaveCount(0);
      await page.clock.runFor(1100);
      await expect.poll(() => pending !== undefined).toBe(true);

      // Run every interval, including the retired two-second poll, without a wall-clock sleep.
      await page.clock.runFor(10_000);
      expect(sessionReads()).toEqual([]);
      expect(attachments().map(call => call.body.sessionFile)).toEqual([tabs[0].sessionFile]);
      expect(engine.calls.filter(call => /\/sessions\/[^/]+\/events$/.test(call.path))).toHaveLength(1);

      await publish(pending!, { epoch: 'background-test', seq: 2, type: 'world', at: '2026-10-10T12:00:01Z', payload: {
        rows: [{ ...rows[1], running: true, needsYou: 1 }], removed: [],
      } });
      pending = undefined;
      const dot = background.getByRole('img', { name: 'Needs you', exact: true });
      await expect(dot).toHaveAttribute('data-state', 'waiting');
      await expect(dot).toHaveCSS('background-color', await tokenColorIn(background, 'amber'));
      await expect(dot).toHaveCSS('width', '6px');
      await expect(dot).toHaveCSS('height', '6px');
      await expect(background).toHaveAttribute('aria-description', 'Needs you');
      await expect(background).toHaveAttribute('aria-selected', 'false');
      await expect(reading).toHaveAttribute('aria-selected', 'true');
      await expect(quiet.locator('.tab-dot')).toHaveCount(0);
      expect(sessionReads()).toEqual([]);
      expect(attachments()).toHaveLength(1);

      await expectNoHorizontalOverflow(page);
      await page.clock.resume();
      await expectAccessible(page);
      // The waiting tab remains reachable by the strip's keyboard path even when it overflows.
      await reading.focus();
      await reading.press('ArrowRight');
      await expect(background).toBeFocused();
      await expect(background).toHaveAttribute('aria-selected', 'true');
      await expect.poll(() => attachments().map(call => call.body.sessionFile)).toEqual([tabs[0].sessionFile, tabs[1].sessionFile]);
    });
  }
}

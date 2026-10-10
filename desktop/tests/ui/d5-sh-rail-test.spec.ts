import { test, expect, type Page } from '@playwright/test';
import type { AttentionItem, WorldRow } from '../../src/features/chat/world-client';
import { expectAccessible, tokenColor } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type MockPlaces } from './support/mock-places';
import { installNativeHttpMock } from './support/native-http-mock';

// SH-024..031, SH-033, SH-041, SH-044, SH-086.
// Iteration 2 I2.1 retired the Inbox row and tab. Needs-you across conversations is the frame pill.
// Now stays a rail row: its count is unplaced chats, its dot is unplaced needs-you, and a modified click
// opens a window on Now. These assertions fail if that row, the pill, or the retirement is removed.

const reading = 'pl_0123456789abcdef';
const older = 'unplaced-wait';
const newer = 'placed-wait';
const quiet = 'quiet-unplaced';
const widths = [320, 600, 850, 1200] as const;

type Feed = 'empty' | 'quiet' | 'waiting';

const rail = (page: Page) => page.locator('.place-rail');
const nowRow = (page: Page) => rail(page).locator('[data-rail-item="now"]');
const pill = (page: Page) => page.getByRole('button', { name: /need you elsewhere/ });
/** Computed time is 120ms in one engine and 0.12s in the other. Both are the token. */
const millis = (value: string) => { const n = parseFloat(value); return value.trim().endsWith('ms') ? n : n * 1000; };

function row(session: string, title: string, needsYou: boolean): WorldRow {
  return {
    session, title, sessionFile: sessionFileFor(session), project: 'app', workspace: '/w', sourceFolders: [],
    state: needsYou ? 'waiting on you' : 'open', live: true, open: true, running: false, needsYou, failed: 0,
    tasks: { running: 0, incomplete: 0, done: 0, failed: 0, total: 0 },
  };
}

// Listed newest first on purpose. The queue must still put the older question first.
function attention(): AttentionItem[] {
  return [
    { key: 'q:newer', session: newer, kind: 'consent', id: 2, text: 'Approve the deploy', sourceFolders: [], title: 'Filed question', answerable: false, asked: '2026-03-01T00:00:00.000Z', placeIds: [reading], placeNames: ['Reading'] },
    { key: 'q:older', session: older, kind: 'consent', id: 1, text: 'Pick a database', sourceFolders: [], title: 'Oldest question', answerable: false, asked: '2026-01-01T00:00:00.000Z' },
  ];
}

/** The world stream the pill reads. Registered after the places mock so this feed wins for /world and /events. */
async function installAttention(page: Page) {
  const items = attention();
  const rows = [row(older, 'Oldest question', true), row(newer, 'Filed question', true), row(quiet, 'Quiet draft', false)];
  const seq = 1;
  const reset = { seq, type: 'reset', payload: { rows, items } };
  await page.route('**/api/engine/world', route => route.fulfill({ json: { seq, rows, items } }));
  await page.route('**/api/engine/events?*', async route => {
    const after = Number(new URL(route.request().url()).searchParams.get('after'));
    if (after >= seq) await new Promise(resolve => setTimeout(resolve, 100));
    await route.fulfill({
      contentType: 'text/event-stream',
      body: after >= seq ? ': connected\n\n' : `id: ${seq}\ndata: ${JSON.stringify(reset)}\n\n`,
    });
  });
}

async function installWindowStub(page: Page) {
  await page.addInitScript(() => {
    const calls: { cmd: string; args: unknown }[] = [];
    const callbacks = new Map<number, (data: unknown) => void>();
    let nextId = 1;
    async function invoke(cmd: string, args: Record<string, unknown> = {}) {
      calls.push({ cmd, args });
      if (cmd.startsWith('plugin:http|')) return (window as unknown as { __engineHttpInvoke: (cmd: string, args: Record<string, unknown>) => Promise<unknown> }).__engineHttpInvoke(cmd, args);
      if (cmd === 'plugin:event|listen') return args.handler;
      if (cmd === 'engine_connection') return { url: location.origin, token: 'mock-token', model: 'deepseek/deepseek-v4.1-flash' };
      if (cmd === 'window_context') return { label: 'main', placeKey: 'now' };
      if (cmd === 'window_list') return [{ label: 'main', placeKey: 'now', focused: true, title: 'codeaf' }];
      if (cmd === 'window_claim_handoff') return null;
      if (cmd === 'window_open') return 'w-2';
      return null;
    }
    Object.assign(window, { isTauri: true, __menuWindow: { calls } });
    Object.assign(window, {
      __TAURI_INTERNALS__: {
        invoke,
        transformCallback(callback: (data: unknown) => void) { const id = nextId++; callbacks.set(id, callback); return id; },
        unregisterCallback(id: number) { callbacks.delete(id); },
        metadata: { currentWindow: { label: 'main' }, currentWebview: { windowLabel: 'main', label: 'main' } },
      },
      __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener() { /* the stub holds no listener */ } },
    });
  });
}

async function boot(page: Page, theme: 'light' | 'dark', feed: Feed, native = false): Promise<MockPlaces> {
  if (native) { await installNativeHttpMock(page); await installWindowStub(page); }
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.addInitScript(() => {
    Object.assign(window, { __openedUrls: [] as string[], open: (url: string) => {
      (window as unknown as { __openedUrls: string[] }).__openedUrls.push(url);
      return null;
    } });
  });
  await installMockEngine(page, {});
  const chats = feed === 'empty' ? [] : feed === 'quiet'
    ? [{ id: quiet, title: 'Quiet draft' }]
    : [
      { id: quiet, title: 'Quiet draft' },
      { id: older, title: 'Oldest question', needsYou: true, reason: 'Pick a database', at: '2026-01-01T00:00:00.000Z' },
      { id: newer, title: 'Filed question', places: [reading], needsYou: true, reason: 'Approve the deploy', at: '2026-03-01T00:00:00.000Z' },
    ];
  const places = await installMockPlaces(page, { places: [{ id: reading, name: 'Reading', pinned: true, tint: 'sage' }], chats });
  if (feed === 'waiting') await installAttention(page);
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  // At ≤600 the rail is inside a closed drawer, so Now is mounted but not shown.
  const narrow = (page.viewportSize()?.width ?? 1200) <= 600;
  if (narrow) await expect(page.getByRole('button', { name: 'Show sidebar' })).toBeVisible();
  else {
    await expect(nowRow(page)).toBeVisible();
    // Reading arrives with the graph. Now's count is from that same read, so later absence checks are not a race.
    await expect(page.getByRole('button', { name: /^Reading/ }).first()).toBeVisible();
  }
  return places;
}

async function tokenWidth(page: Page, name: string) {
  return page.evaluate(token => {
    const probe = document.createElement('div');
    probe.style.width = `var(--${token})`;
    document.body.append(probe);
    const width = getComputedStyle(probe).width;
    probe.remove();
    return width;
  }, name);
}

async function focusRing(page: Page) {
  return page.evaluate(() => {
    const probe = document.createElement('span');
    probe.style.boxShadow = '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 var(--focus-halo-width) var(--accent-soft)';
    document.body.append(probe);
    const value = getComputedStyle(probe).boxShadow;
    probe.remove();
    return value;
  });
}

const opens = (page: Page) => page.evaluate(() => (window as unknown as { __menuWindow: { calls: { cmd: string; args: unknown }[] } }).__menuWindow.calls.filter(call => call.cmd === 'window_open'));

async function retired(page: Page, scope: ReturnType<typeof rail>) {
  await expect(scope.getByRole('button', { name: 'Inbox', exact: true })).toHaveCount(0);
  await expect(scope.getByRole('button', { name: 'Activity', exact: true })).toHaveCount(0);
  await expect(scope.getByRole('button', { name: 'Find anything', exact: true })).toHaveCount(0);
  await expect(scope.locator('.address-field, .sidebar-bottom, .brand-mark')).toHaveCount(0);
  await expect(scope.getByRole('combobox', { name: 'Theme' })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
  await expect(scope.getByText('Find anything')).toHaveCount(0);
}

test.describe('rail Now, retired Inbox, compact width', () => {
  test.describe.configure({ timeout: 60_000 });

  for (const theme of ['light', 'dark'] as const) {
    test(`no unplaced chats draw no Now count, dot or pill · ${theme}`, async ({ page }) => {
      const empty = await boot(page, theme, 'empty');
      await expect.poll(() => empty.calls.some(call => call.path === '/events' || call.path === '/world')).toBe(true);
      await expect(nowRow(page).locator('[data-rail-count]')).toHaveCount(0);
      await expect(nowRow(page).locator('.status-mark')).toHaveCount(0);
      await expect(pill(page)).toHaveCount(0);
      await expect(rail(page).getByText(/^0$/)).toHaveCount(0);
      await retired(page, rail(page));
    });

    test(`a quiet unplaced chat shows Now's count and still no needs-you dot · ${theme}`, async ({ page }) => {
      const quietFeed = await boot(page, theme, 'quiet');
      await expect.poll(() => quietFeed.calls.some(call => call.path === '/events' || call.path === '/world')).toBe(true);
      await expect(nowRow(page).locator('[data-rail-count]')).toHaveText('1');
      await expect(nowRow(page)).toHaveAttribute('aria-description', '1 unplaced');
      await expect(nowRow(page).locator('.status-mark')).toHaveCount(0);
      await expect(pill(page)).toHaveCount(0);
      await retired(page, rail(page));
    });

    test(`needs-you draws the Now dot and the frame pill; Now switches the window · ${theme}`, async ({ page }) => {
      await boot(page, theme, 'waiting');
      const now = nowRow(page);
      await expect(now.locator('[data-rail-count]')).toHaveText('2');
      await expect(now).toHaveAttribute('aria-description', '2 unplaced');
      await expect(now.locator('[data-icon="now"]')).toBeVisible();
      const measured = await now.evaluate(el => {
        const icon = el.querySelector('.app-icon');
        const count = el.querySelector('[data-rail-count]');
        const style = getComputedStyle(el);
        const countStyle = count ? getComputedStyle(count) : null;
        const iconStyle = icon ? getComputedStyle(icon) : null;
        const box = el.getBoundingClientRect();
        const iconBox = icon?.getBoundingClientRect();
        return {
          height: box.height, fontSize: style.fontSize, borderLeft: style.borderLeftWidth, color: style.color,
          countSize: countStyle?.fontSize, countColor: countStyle?.color,
          icon: iconBox ? { width: iconBox.width, height: iconBox.height } : null, iconColor: iconStyle?.color,
        };
      });
      expect(measured.height).toBe(32);
      expect(measured.fontSize).toBe('13px');
      expect(measured.borderLeft).toBe('0px');
      expect(measured.color).toBe(await tokenColor(page, 'ink'));
      expect(measured.countSize).toBe('11px');
      expect(measured.countColor).toBe(await tokenColor(page, 'ink-3'));
      expect(measured.icon).toEqual({ width: 14, height: 14 });
      expect(measured.iconColor).toBe(await tokenColor(page, 'ink-3'));
      await expect(now).toHaveCSS('background-color', await tokenColor(page, 'tab'));

      const dot = now.locator('.status-mark-dot');
      expect(Math.round((await dot.boundingBox())!.width)).toBe(6);
      expect(Math.round((await dot.boundingBox())!.height)).toBe(6);
      expect(await dot.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(await tokenColor(page, 'amber'));
      await now.locator('.status-mark').hover();
      await expect(page.getByRole('tooltip')).toHaveText('1 needs you in Now');
      await page.mouse.move(0, 0);

      await expect(pill(page)).toBeVisible();
      const glyph = page.locator('.frame-pill-glyph');
      expect(Math.round((await glyph.boundingBox())!.width)).toBe(6);
      expect(await glyph.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(await tokenColor(page, 'amber'));
      await retired(page, rail(page));

      const readingRow = rail(page).locator(`[data-rail-item="${reading}"]`);
      await readingRow.click();
      await expect(page.locator('body')).toHaveAttribute('data-tint', 'sage');
      await expect.poll(() => page.evaluate(() => localStorage.getItem('codeaf.desktop.window.main.place'))).toBe(reading);
      await expect(now).not.toHaveAttribute('aria-current', 'page');
      await now.hover();
      await expect(now).toHaveCSS('background-color', await tokenColor(page, 'tab-hover'));
      expect(millis(await now.evaluate(el => getComputedStyle(el).transitionDuration))).toBe(120);
      const box = (await now.boundingBox())!;
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      await page.mouse.down();
      await expect(now).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
      expect(millis(await now.evaluate(el => getComputedStyle(el).transitionDuration))).toBe(80);
      await page.mouse.up();
      await expect(now).toHaveAttribute('aria-current', 'page');
      await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
      await expect.poll(() => page.evaluate(() => localStorage.getItem('codeaf.desktop.window.main.place'))).toBe('now');
      expect(await now.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe(await focusRing(page));

      await readingRow.click();
      await page.locator('.place-rail [data-rail-item="' + reading + '"]').focus();
      await page.keyboard.press('ArrowUp');
      await expect(now).toBeFocused();
      expect(await now.evaluate(el => getComputedStyle(el).boxShadow)).toBe(await focusRing(page));
      await page.keyboard.press('Enter');
      await expect(now).toHaveAttribute('aria-current', 'page');
      await expect.poll(() => page.evaluate(() => localStorage.getItem('codeaf.desktop.window.main.place'))).toBe('now');

      await now.click({ button: 'right' });
      await expect(page.getByRole('menu')).toHaveCount(0);

      await readingRow.click();
      await now.click({ button: 'middle' });
      await expect(page.locator('body')).toHaveAttribute('data-tint', 'sage');
      await now.click({ modifiers: ['Control'] });
      await expect(now).toHaveAttribute('aria-current', 'page');
      await readingRow.click();
      await now.click({ modifiers: ['Meta'] });
      await expect(now).toHaveAttribute('aria-current', 'page');
      await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
      expect(await page.evaluate(() => (window as unknown as { __openedUrls: string[] }).__openedUrls)).toEqual([]);
      await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);

      // The feed lists the newer question first. Hover names the count, and the older question is still first.
      await pill(page).hover();
      const queue = page.getByRole('group', { name: /2 need you/ });
      await expect(queue).toBeVisible();
      await expect(queue.locator('.queue-popover-row').first()).toContainText('Pick a database');
      await expect(queue.locator('.queue-popover-row').nth(1)).toContainText('Approve the deploy');
      await queue.getByRole('button', { name: /Pick a database/ }).click();
      await expect(page.getByRole('tab', { name: 'Oldest question', exact: true })).toHaveAttribute('aria-selected', 'true');
      await expect(pill(page)).toHaveAccessibleName(/1 need you elsewhere/);
      await expect(page.getByRole('tab', { name: 'Inbox', exact: true })).toHaveCount(0);
    });

    test(`native command, control and middle click open Now without leaving this window · ${theme}`, async ({ page }) => {
      await boot(page, theme, 'quiet', true);
      await rail(page).locator(`[data-rail-item="${reading}"]`).click();
      await expect(page.locator('body')).toHaveAttribute('data-tint', 'sage');
      const now = nowRow(page);
      await now.click({ modifiers: ['Control'] });
      await now.click({ modifiers: ['Meta'] });
      await now.click({ button: 'middle' });
      await expect.poll(() => opens(page)).toEqual([
        { cmd: 'window_open', args: { request: { placeKey: 'now' } } },
        { cmd: 'window_open', args: { request: { placeKey: 'now' } } },
        { cmd: 'window_open', args: { request: { placeKey: 'now' } } },
      ]);
      await expect(page.locator('body')).toHaveAttribute('data-tint', 'sage');
      await expect(page.getByRole('button', { name: /^Reading/ }).first()).toHaveAttribute('aria-current', 'page');
      await now.click({ button: 'right' });
      await expect(page.getByRole('menu')).toHaveCount(0);
      await expect.poll(() => opens(page)).toHaveLength(3);
    });

    for (const width of widths) {
      test(`rail at ${width}px · ${theme}`, async ({ page }) => {
        await page.setViewportSize({ width, height: width <= 600 ? 480 : 800 });
        await boot(page, theme, width <= 600 ? 'waiting' : 'quiet');
        if (width > 600) {
          const side = page.locator('.app-shell > .sidebar');
          const px = Math.round((await side.boundingBox())!.width);
          if (width <= 850) {
            expect(px).toBe(190);
            expect(`${px}px`).toBe(await tokenWidth(page, 'sidebar-width-compact'));
          } else {
            expect(px).toBeGreaterThan(190);
            expect(`${px}px`).toBe(await tokenWidth(page, 'sidebar-width'));
          }
          await expect(nowRow(page)).toBeVisible();
          await retired(page, rail(page));
          await expectAccessible(page);
          return;
        }
        await expect(nowRow(page)).toBeHidden();
        const show = page.getByRole('button', { name: 'Show sidebar' });
        await show.click();
        const drawer = page.getByRole('dialog', { name: 'Navigation', exact: true });
        await expect(drawer.getByRole('button', { name: 'Hide sidebar' })).toBeFocused();
        await expect(page.getByRole('button', { name: /^Reading/ }).first()).toBeVisible();
        await expect(nowRow(page)).toBeVisible();
        await expect(nowRow(page).locator('[data-rail-count]')).toHaveText('2');
        await expect(nowRow(page).locator('.status-mark-dot')).toBeVisible();
        await expect(pill(page)).toBeVisible();
        await retired(page, drawer.locator('.place-rail'));
        await page.locator('.workspace-tab-actions').getByRole('button', { name: 'New tab', exact: true, includeHidden: true }).evaluate(el => (el as HTMLElement).focus());
        await expect(drawer.getByRole('button', { name: 'Hide sidebar' })).toBeFocused();
        await expectAccessible(page);
        const ring = await focusRing(page);
        await page.keyboard.press('Tab');
        const focused = page.locator(':focus');
        const keyboard = await focused.evaluate(el => ({ inside: !!el.closest('dialog'), shadow: getComputedStyle(el).boxShadow }));
        expect(keyboard.inside).toBe(true);
        expect(keyboard.shadow).toBe(ring);
        await page.keyboard.press('Escape');
        await expect(drawer).not.toBeVisible();
        await expect(show).toBeFocused();
        await show.click();
        await page.getByRole('button', { name: /^Reading/ }).first().click();
        await expect(drawer).not.toBeVisible();
        await expect.poll(() => page.evaluate(() => localStorage.getItem('codeaf.desktop.window.main.place'))).toBe(reading);
        await show.click();
        await nowRow(page).click();
        await expect(drawer).not.toBeVisible();
        await expect.poll(() => page.evaluate(() => localStorage.getItem('codeaf.desktop.window.main.place'))).toBe('now');
      });
    }
  }
});

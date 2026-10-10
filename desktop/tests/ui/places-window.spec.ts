import { test, expect, type Page } from '@playwright/test';
import { tokenColor } from './contracts';
import { installMockEngine, type MockEngine, type Scenario } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type PlacesSeed } from './support/mock-places';

// Places window (PL-023, PL-024, PL-083, PL-084, PL-096, PL-099, PL-251, PL-259, PL-260).
// The frame wears the place tint, Go to brings that place's strip back onto Home, the collapsed Home tab
// is the switcher, the left edge peeks the rail, and two windows share one tab set. Strings are the design's.

const READING = 'pl_aaaaaaaaaaaaaaaa';
const MARKETING = 'pl_bbbbbbbbbbbbbbbb';
const SOFTWARE = 'pl_dddddddddddddddd';
const CONFIG = 'pl_cccccccccccccccc';
const WIDTHS = [320, 600, 850, 1200] as const;

const frameOf = { light: { l: 0.93, c: 0.035 }, dark: { l: 0.27, c: 0.035 } };
const hueOf = { graphite: 250, rose: 12, iris: 285, tide: 240 };
const chromaOf = { graphite: 0.012, rose: 0.13, iris: 0.14, tide: 0.13 };
const glassAlpha = { light: 0.8, dark: 0.84 };

type Tint = keyof typeof hueOf;
type Theme = 'light' | 'dark';

const garden = (): PlacesSeed => ({
  places: [
    { id: READING, name: 'Reading', tint: 'iris', pinned: true },
    { id: MARKETING, name: 'Marketing', tint: 'rose', pinned: true },
    { id: SOFTWARE, name: 'Software', tint: 'tide', pinned: true },
    { id: CONFIG, name: 'Config parser', parents: ['Reading'], lastOpenedAt: 'now' },
  ],
  chats: [
    { id: 'launch', title: 'Launch copy', places: ['Marketing', 'Software'], live: true, doing: 'working' },
    { id: 'need', title: 'Allow the push', places: ['Config parser'], needsYou: true, reason: 'Allow the push?' },
  ],
});

type Pane = { id?: string; kind?: string; sessionFile?: string; split?: { panes?: Pane[] } };
type Doc = { key: string; revision: number; writer?: string; workspace: Record<string, unknown> | null };

/** One tab-set store for every page that installs it, so two windows see one place. */
function workspaceHub(seed: Record<string, Record<string, unknown>> = {}, names: Record<string, string> = {}) {
  const docs = new Map<string, Doc>();
  const waiters = new Set<() => void>();
  for (const [key, workspace] of Object.entries(seed)) docs.set(key, { key, revision: 1, workspace });
  const wake = () => { for (const waiter of [...waiters]) waiter(); };
  const panesOf = (doc: Doc | undefined): Pane[] => {
    const tabs = (doc?.workspace?.tabs ?? []) as Pane[];
    return tabs.flatMap(tab => tab.split?.panes ?? [tab]);
  };
  const hold = (key: string, after: number) => new Promise<void>(resolve => {
    if ((docs.get(key)?.revision ?? 0) > after) { resolve(); return; }
    let settled = false;
    const finish = () => { if (settled) return; settled = true; clearTimeout(timer); waiters.delete(onChange); resolve(); };
    const onChange = () => { if ((docs.get(key)?.revision ?? 0) > after) finish(); };
    const timer = setTimeout(finish, 250);
    waiters.add(onChange);
    if ((docs.get(key)?.revision ?? 0) > after) finish();
  });
  return {
    docs,
    async install(page: Page) {
      await page.route('**/api/engine/workspaces/**', async route => {
        try {
          const request = route.request();
          const url = new URL(request.url());
          const parts = url.pathname.split('/').filter(Boolean);
          const key = decodeURIComponent(parts[parts.indexOf('workspaces') + 1] ?? '');
          if (!/^(now|pl_[0-9a-f]{16})$/.test(key)) {
            await route.fulfill({ status: 404, json: { error: 'there is no such tab set', code: 'unknown_key' } });
            return;
          }
          if (parts[parts.indexOf('workspaces') + 2] === 'open-elsewhere') {
            const current = docs.get(key);
            const pane = panesOf(current).find(item => item.id === url.searchParams.get('pane'));
            const places = !pane?.sessionFile || pane.kind !== 'conversation' ? [] : [...docs.entries()].flatMap(([id, doc]) => {
              if (id === key || !names[id]) return [];
              const hit = panesOf(doc).some(item => item.kind === 'conversation' && item.sessionFile === pane.sessionFile);
              return hit ? [{ id, name: names[id] }] : [];
            });
            await route.fulfill({ json: { places } });
            return;
          }
          const method = request.method();
          const current = docs.get(key) ?? { key, revision: 0, workspace: null };
          if (method === 'PUT') {
            const body = request.postDataJSON() as { revision?: number; writer?: string; workspace?: Record<string, unknown> };
            if (body.revision !== current.revision) {
              await route.fulfill({ status: 409, json: { error: 'these tabs changed in another window', code: 'conflict', current } });
              return;
            }
            const saved = { key, revision: current.revision + 1, writer: String(body.writer ?? ''), workspace: body.workspace ?? null };
            docs.set(key, saved);
            wake();
            await route.fulfill({ json: saved });
            return;
          }
          if (method !== 'GET') { await route.fulfill({ status: 405, json: { error: 'GET or PUT required' } }); return; }
          if (url.searchParams.get('wait')) await hold(key, Number(url.searchParams.get('after') ?? -1));
          await route.fulfill({ json: docs.get(key) ?? current });
        } catch {
          // The page closed while a long poll was waiting. There is nothing left to answer.
        }
      });
    },
  };
}

const tab = (fields: Record<string, unknown>) => ({ titleSource: 'manual', pinned: false, draft: '', ...fields });

function readingStrip(sessionFile?: string) {
  return {
    schema: 1,
    tabs: [
      tab({ id: 'home-reading', kind: 'home', place: READING, title: 'Reading', pinned: true }),
      tab({ id: 'tab-span', kind: 'conversation', title: 'Spanner TrueTime', ...(sessionFile ? { sessionFile } : {}) }),
      tab({ id: 'tab-web', kind: 'web', title: 'raft.github.io', target: { url: 'https://raft.github.io/' } }),
    ],
    groups: [],
    closed: [],
    nextNumber: 4,
  };
}

const rail = (page: Page) => page.locator('.app-shell .place-rail').first();
const homeSelect = (page: Page) => page.locator('.home-tab-select');
const stops = (engine: MockEngine) => engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/stop'));

async function boot(page: Page, theme: Theme, seed: PlacesSeed = garden(), scenario?: Scenario, hub?: ReturnType<typeof workspaceHub>) {
  await page.emulateMedia({ colorScheme: theme, reducedMotion: theme === 'dark' ? 'reduce' : 'no-preference' });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  const engine = await installMockEngine(page, scenario ?? { initial: { entries: [], title: '' } });
  await installMockPlaces(page, seed);
  if (hub) await hub.install(page);
  await page.goto('/');
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  await expect(page).toHaveTitle('codeaf');
  return { engine, mac, slot: (n: number) => (mac ? `Control+Digit${n}` : `Alt+Digit${n}`), mod: mac ? 'Meta' : 'Control', shortcut: (n: number) => (mac ? `⌃${n}` : `Alt+${n}`) };
}

async function probeColor(page: Page, value: string) {
  return page.evaluate(color => {
    const probe = document.createElement('span');
    probe.style.color = color;
    document.body.append(probe);
    const resolved = getComputedStyle(probe).color;
    probe.remove();
    return resolved;
  }, value);
}

/** The frame's painted colour: the shell when it is opaque, otherwise the strip that wears the glass. */
async function paintedFrame(page: Page) {
  return page.locator('.app-shell').evaluate(shell => {
    const bar = document.querySelector('.workspace-tabbar');
    const shellBg = getComputedStyle(shell).backgroundColor;
    const transparent = shellBg === 'rgba(0, 0, 0, 0)' || shellBg === 'transparent';
    const target = transparent && bar ? bar : shell;
    return { background: getComputedStyle(target).backgroundColor, animations: target.getAnimations().length, material: document.documentElement.dataset.material ?? '' };
  });
}

async function expectTint(page: Page, theme: Theme, tint: Tint) {
  await expect(page.locator('body')).toHaveAttribute('data-tint', tint);
  await expect(page.locator('.app-shell')).toHaveAttribute('data-tint', tint);
  const body = await page.evaluate(() => {
    const style = getComputedStyle(document.body);
    return { h: style.getPropertyValue('--h').trim(), a: style.getPropertyValue('--a').trim() };
  });
  expect(Number(body.h)).toBeCloseTo(hueOf[tint], 0);
  expect(Number(body.a)).toBeCloseTo(chromaOf[tint], 3);
  const { l, c } = frameOf[theme];
  const h = hueOf[tint];
  const solid = await probeColor(page, `oklch(${l} ${c} ${h})`);
  const glass = await probeColor(page, `oklch(${l} ${c} ${h} / ${glassAlpha[theme]})`);
  await expect.poll(async () => {
    const painted = await paintedFrame(page);
    return painted.background === solid || painted.background === glass;
  }).toBe(true);
  await expect.poll(async () => (await paintedFrame(page)).animations).toBe(0);
  const material = (await paintedFrame(page)).material;
  expect(['glass', 'solid']).toContain(material);
  await expect(page).toHaveTitle('codeaf');
  await expect(page.locator('.titlebar, .traffic-lights')).toHaveCount(0);
}

async function expectNoOverflow(page: Page) {
  expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= window.innerWidth)).toBe(true);
}

async function ringShadow(page: Page, kind: 'rail' | 'menu') {
  return page.evaluate(which => {
    const probe = document.createElement('span');
    probe.style.boxShadow = which === 'menu'
      ? '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 calc(var(--focus-ring-width) + var(--focus-halo-width)) var(--accent-soft)'
      : '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 var(--focus-halo-width) var(--accent-soft)';
    document.body.append(probe);
    const value = getComputedStyle(probe).boxShadow;
    probe.remove();
    return value;
  }, kind);
}

async function tabTo(page: Page, selector: string) {
  const target = page.locator(selector);
  for (let step = 0; step < 40; step++) {
    if (await target.evaluate(el => el === document.activeElement).catch(() => false)) return;
    await page.keyboard.press('Tab');
  }
  await expect(target).toBeFocused();
}

const freezeClock = async (page: Page) => {
  await page.clock.install({ time: new Date('2026-10-09T12:00:00.000Z') });
  await page.clock.pauseAt('2026-10-09T12:00:02.000Z');
};

for (const theme of ['light', 'dark'] as const) {
  test(`PL-023 PL-024 PL-259 PL-260 the frame wears the place tint in ${theme}`, async ({ page }) => {
    test.setTimeout(60_000);
    await boot(page, theme);
    const duration = await page.locator('.app-shell').evaluate(el => parseFloat(getComputedStyle(el).transitionDuration));
    // Iteration 2 holds the tint swap for 320ms. Reduced motion (this dark pass) makes that token 0.
    expect(duration).toBe(theme === 'dark' ? 0 : 0.32);
    await expectTint(page, theme, 'graphite');
    await expect(page.locator('.shell-hotzone[data-edge="left"]')).toHaveCount(0);

    await rail(page).getByRole('button', { name: /^Marketing/ }).click();
    await expectTint(page, theme, 'rose');
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      await expectTint(page, theme, 'rose');
      await expectNoOverflow(page);
      // The column eases between widths. Wait until it has arrived at the token, not a frame of that ease.
      if (width >= 850) await expect.poll(async () => Math.round((await page.locator('.app-shell > .sidebar').boundingBox())?.width ?? 0)).toBe(width === 1200 ? 252 : 190);
      else await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toBeHidden();
    }
    await page.setViewportSize({ width: 1200, height: 800 });

    // Config parser has no tint of its own. It inherits Reading, so the frame is iris.
    await rail(page).getByRole('button', { name: /^Config parser/ }).click();
    await expectTint(page, theme, 'iris');
    await rail(page).getByRole('button', { name: 'All places' }).click();
    await expect(page.getByRole('tab', { name: 'All places', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expectTint(page, theme, 'iris');
    await rail(page).getByRole('button', { name: 'Now', exact: true }).click();
    await expectTint(page, theme, 'graphite');
  });

  test(`PL-096 Go to Reading restores its strip and lands on Home in ${theme}`, async ({ page }) => {
    test.setTimeout(60_000);
    const hub = workspaceHub({ [READING]: readingStrip() });
    const rig = await boot(page, theme, garden(), undefined, hub);
    await rail(page).getByRole('button', { name: /^Reading/ }).click();
    await expect(page.getByRole('heading', { name: 'Reading', exact: true })).toBeVisible();
    await expect(homeSelect(page)).toHaveAttribute('aria-selected', 'true');
    await expectTint(page, theme, 'iris');
    await expect(page.getByRole('tab', { name: 'Spanner TrueTime', exact: true })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'raft.github.io', exact: true })).toBeVisible();

    await page.getByRole('tab', { name: 'raft.github.io', exact: true }).click();
    await expect(page.getByText('Web pages open in the desktop app', { exact: true })).toBeVisible();
    await expect(page.getByText('This window cannot show raft.github.io.', { exact: true })).toBeVisible();
    await expect(page.locator('.web-state').getByRole('button', { name: 'Open in browser', exact: true })).toBeVisible();

    await rail(page).getByRole('button', { name: 'All places' }).click();
    await expect(page.getByRole('tab', { name: 'All places', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('tab', { name: 'Spanner TrueTime', exact: true })).toBeVisible();
    await expectTint(page, theme, 'iris');
    // Leave only after the strip is saved. Closing the place drops a save that has not been sent yet.
    await expect.poll(() => JSON.stringify(hub.docs.get(READING)?.workspace ?? {})).toContain('All places');

    await rail(page).getByRole('button', { name: /^Marketing/ }).click();
    await expect(page.getByRole('heading', { name: 'Marketing', exact: true })).toBeVisible();
    await expectTint(page, theme, 'rose');
    await expect(page.getByRole('tab', { name: 'Spanner TrueTime', exact: true })).toHaveCount(0);
    await expect(page.getByRole('tab', { name: 'raft.github.io', exact: true })).toHaveCount(0);
    await expect(page.getByRole('tab', { name: 'All places', exact: true })).toHaveCount(0);

    await page.keyboard.press(rig.slot(1));
    await expect(page.getByRole('heading', { name: 'Reading', exact: true })).toBeVisible();
    await expect(homeSelect(page)).toHaveAttribute('aria-selected', 'true');
    await expect(page.getByRole('tab', { name: 'Spanner TrueTime', exact: true })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'raft.github.io', exact: true })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'All places', exact: true })).toBeVisible();
    await expectTint(page, theme, 'iris');
    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      // At 600 and under the rail is a drawer, so the Home tab also carries the needs-you name.
      await expect(page.getByRole('tab', { name: /^Reading/ })).toBeVisible();
      await expect(page.getByRole('tab', { name: 'Spanner TrueTime', exact: true })).toBeVisible();
      await expect(page.getByRole('tab', { name: 'raft.github.io', exact: true })).toBeVisible();
      await expectNoOverflow(page);
    }
  });

  test(`PL-083 PL-084 the slot keys and the collapsed switcher in ${theme}`, async ({ page }) => {
    test.setTimeout(90_000);
    const rig = await boot(page, theme);
    await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toHaveAttribute('aria-current', 'page');
    await rail(page).getByRole('button', { name: 'Now', exact: true }).focus();
    await page.keyboard.press('ArrowDown');
    const focusedRow = page.locator('.rail-row-main:focus');
    await expect(focusedRow).toBeFocused();
    expect(await focusedRow.evaluate(el => el.matches(':focus-visible'))).toBe(true);
    expect(await page.evaluate(() => document.documentElement.dataset.input ?? '')).toBe('');
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--focus-ring-width').trim())).toBe('2px');
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--focus-halo-width').trim())).toBe('4px');
    await expect(focusedRow).toHaveCSS('box-shadow', await ringShadow(page, 'rail'));

    await page.keyboard.press(rig.slot(1));
    await expect(rail(page).getByRole('button', { name: /^Reading/ })).toHaveAttribute('aria-current', 'page');
    await expect(page.getByRole('heading', { name: 'Reading', exact: true })).toBeVisible();
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'iris');
    await page.keyboard.press(rig.slot(2));
    await expect(rail(page).getByRole('button', { name: /^Marketing/ })).toHaveAttribute('aria-current', 'page');
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'rose');
    await page.keyboard.press(rig.slot(0));
    await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toHaveAttribute('aria-current', 'page');
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
    await page.keyboard.press(rig.slot(1));
    // A click focuses the row, but the keyboard ring stays off while the pointer owns the input.
    const software = rail(page).getByRole('button', { name: /^Software/ });
    await software.click();
    expect(await page.evaluate(() => document.documentElement.dataset.input)).toBe('pointer');
    expect(await software.evaluate(el => getComputedStyle(el).boxShadow)).not.toBe(await ringShadow(page, 'rail'));
    await page.keyboard.press(rig.slot(1));

    await page.keyboard.press(`${rig.mod}+s`);
    await expect(page.locator('.app-shell')).toHaveClass(/sidebar-collapsed/);
    await tabTo(page, '.home-tab-select');
    await expect(homeSelect(page)).toHaveAttribute('aria-haspopup', 'menu');
    await expect(homeSelect(page)).toHaveAttribute('aria-description', '1 needs you in Config parser');
    await page.keyboard.press('Enter');
    const menu = page.getByRole('menu', { name: 'Place switcher' });
    await expect(menu).toBeVisible();
    await page.keyboard.press('ArrowDown');
    const focusedItem = page.locator('.place-switcher .menu-item:focus');
    await expect(focusedItem).toBeFocused();
    // The menu's own focus move follows the key. Wait until the browser treats that focus as visible.
    await expect.poll(async () => focusedItem.evaluate(el => el.matches(':focus-visible'))).toBe(true);
    await expect(focusedItem).toHaveCSS('box-shadow', await ringShadow(page, 'menu'));
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Now/ })).toContainText(rig.shortcut(0));
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Reading/ })).toContainText(rig.shortcut(1));
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Config parser/ })).toContainText(rig.shortcut(4));
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Reading/ })).toHaveAttribute('aria-checked', 'true');
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Reading/ })).toHaveCSS('background-color', await tokenColor(page, 'field-2'));
    await expect(menu.getByText('Pinned', { exact: true })).toBeVisible();
    await expect(menu.getByText('Open', { exact: true })).toBeVisible();
    await expect(menu.locator('.rail-place-path')).toHaveText(' · Reading');
    // The child needs you, and Reading carries that same roll-up, so both rows say it.
    await expect(menu.getByRole('menuitemcheckbox', { name: /1 needs you in Config parser/ })).toHaveCount(2);
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Config parser/ }).getByRole('img', { name: '1 needs you in Config parser' })).toBeVisible();
    await expect(menu.getByRole('menuitem', { name: /^All places/ })).toBeVisible();
    await expect(menu.getByText('Inbox')).toHaveCount(0);
    await expect(menu.locator('[role=menuitem], [role=menuitemcheckbox]')).toHaveCount(6);
    // Subpixel layout reports a fraction under the token. The painted size is still 290 by 32.
    expect(Math.round((await menu.boundingBox())!.width)).toBe(290);
    expect(Math.round((await menu.locator('.menu-item').first().boundingBox())!.height)).toBe(32);
    await expect(menu.locator('.menu-item').first()).toHaveCSS('font-size', '13px');
    await expect(menu.locator('.menu-item').first()).toHaveCSS('border-top-left-radius', '7px');
    await expect(menu).toHaveCSS('border-top-left-radius', '12px');
    const homeBox = (await page.locator('.home-tab').boundingBox())!;
    const menuBox = (await menu.boundingBox())!;
    expect(Math.abs(menuBox.x - homeBox.x)).toBeLessThanOrEqual(8);
    expect(menuBox.y).toBeGreaterThanOrEqual(homeBox.y + homeBox.height - 2);
    await page.keyboard.press('Escape');
    await expect(menu).toBeHidden();
    await expect(homeSelect(page)).toBeFocused();

    for (const width of [600, 320] as const) {
      await page.setViewportSize({ width, height: 800 });
      await expect(page.locator('.shell-hotzone[data-edge="left"]')).toHaveCount(0);
      await expect(page.locator('.shell-hotzone[data-edge="top"]')).toHaveCount(0);
      await expect(page.getByRole('dialog', { name: 'Navigation' })).toHaveCount(0);
      await expect(homeSelect(page)).toHaveAttribute('aria-haspopup', 'menu');
      await homeSelect(page).click();
      await expect(menu).toBeVisible();
      await expect(menu.getByRole('menuitemcheckbox', { name: /^Now/ })).toBeVisible();
      await expect(menu.getByRole('menuitemcheckbox', { name: /1 needs you in Config parser/ })).toHaveCount(2);
      const narrowMenu = (await menu.boundingBox())!;
      expect(narrowMenu.x).toBeGreaterThanOrEqual(0);
      expect(narrowMenu.x + narrowMenu.width).toBeLessThanOrEqual(width);
      await page.keyboard.press('Escape');
      await expectNoOverflow(page);
    }
  });

  test(`PL-084 PL-260 the left edge peeks the rail after 300ms in ${theme}`, async ({ page }) => {
    test.setTimeout(60_000);
    const rig = await boot(page, theme);
    for (const width of [320, 600] as const) {
      await page.setViewportSize({ width, height: 800 });
      await expect(page.locator('.shell-hotzone[data-edge="left"]')).toHaveCount(0);
      await page.mouse.move(4, 400);
      await page.waitForTimeout(400);
      await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');
    }
    await page.setViewportSize({ width: 1200, height: 800 });
    await page.mouse.move(700, 400);
    await page.keyboard.press(`${rig.mod}+s`);
    await expect(page.locator('.app-shell > .sidebar')).toBeHidden();
    await expect(page.locator('.shell-hotzone[data-edge="top"]')).toHaveCount(0);
    const zone = page.locator('.shell-hotzone[data-edge="left"]');
    expect(await zone.evaluate(el => parseFloat(getComputedStyle(el).width))).toBe(8);
    await page.mouse.move(400, 3);
    await page.waitForTimeout(400);
    await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');
    await page.mouse.move(4, 400);
    await page.mouse.move(700, 400);
    await page.waitForTimeout(400);
    await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');

    await freezeClock(page);
    await page.mouse.move(4, 400);
    await page.clock.fastForward(299);
    await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');
    await page.clock.fastForward(1);
    await expect(page.locator('.app-shell')).toHaveAttribute('data-peek', 'rail');
    expect((await page.locator('.app-shell > .sidebar').boundingBox())!.width).toBe(252);
    await page.mouse.move(700, 400);
    await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');

    await page.setViewportSize({ width: 850, height: 800 });
    await page.mouse.move(4, 300);
    await page.clock.fastForward(299);
    await expect(page.locator('.app-shell')).not.toHaveAttribute('data-peek');
    await page.clock.fastForward(1);
    await expect(page.locator('.app-shell')).toHaveAttribute('data-peek', 'rail');
    const motion = await page.locator('.app-shell > .sidebar').evaluate(el => {
      const style = getComputedStyle(el);
      const box = el.getBoundingClientRect();
      return { durations: style.transitionDuration.split(',').map(part => parseFloat(part)), x: box.x, width: box.width };
    });
    expect(motion.width).toBe(190);
    if (theme === 'dark') {
      expect(motion.durations.every(part => part === 0)).toBe(true);
      expect(motion.x).toBe(0);
    } else expect(motion.durations.some(part => part > 0)).toBe(true);
  });

  test(`PL-099 the same chat stays one run in two places in ${theme}`, async ({ page }) => {
    test.setTimeout(60_000);
    const hub = workspaceHub({}, { [MARKETING]: 'Marketing', [SOFTWARE]: 'Software' });
    const scenario: Scenario = { initial: { entries: [{ Role: 'user', Text: 'Ship the launch copy' }], title: 'Launch copy', running: true, sessionFile: sessionFileFor('launch') } };
    const rig = await boot(page, theme, garden(), scenario, hub);
    const openLaunch = async () => {
      await page.getByRole('region', { name: 'Chats' }).getByRole('button', { name: /Launch copy/ }).click();
      await expect(page.getByRole('tab', { name: 'Launch copy', exact: true })).toHaveAttribute('aria-selected', 'true');
      await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
    };
    await rail(page).getByRole('button', { name: /^Marketing/ }).click();
    const rowMotion = await page.locator('.places-chat-row').first().evaluate(el => getComputedStyle(el).transitionDuration.split(',').map(part => parseFloat(part)));
    if (theme === 'dark') expect(rowMotion.every(part => part === 0)).toBe(true);
    await openLaunch();
    await expect.poll(() => JSON.stringify(hub.docs.get(MARKETING)?.workspace ?? {})).toContain(sessionFileFor('launch'));
    await rail(page).getByRole('button', { name: /^Software/ }).click();
    await openLaunch();
    await expect.poll(() => [...hub.docs.values()].filter(doc => JSON.stringify(doc.workspace).includes(sessionFileFor('launch'))).length).toBe(2);
    await homeSelect(page).click();
    await page.getByRole('tab', { name: 'Launch copy', exact: true }).hover();
    await expect(page.locator('.preview-context')).toHaveText('also open in Marketing');
    const before = stops(rig.engine).length;
    await page.getByRole('button', { name: 'Close Launch copy', exact: true }).click();
    await expect(page.getByRole('tab', { name: 'Launch copy', exact: true })).toHaveCount(0);
    expect(stops(rig.engine)).toHaveLength(before);
    await rail(page).getByRole('button', { name: /^Marketing/ }).click();
    await expect(page.getByRole('tab', { name: 'Launch copy', exact: true })).toBeVisible();
    await page.getByRole('tab', { name: 'Launch copy', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
    expect(stops(rig.engine)).toHaveLength(before);
  });
}

test('PL-251 two windows on Reading share one tab set and keep their own focus', async ({ browser }) => {
  test.setTimeout(90_000);
  for (const theme of ['light', 'dark'] as const) {
    const hub = workspaceHub({ [READING]: readingStrip(sessionFileFor('spanner')) });
    const contexts = await Promise.all([0, 1].map(() => browser.newContext({ viewport: { width: 1200, height: 800 }, colorScheme: theme })));
    try {
      const pages = await Promise.all(contexts.map(context => context.newPage()));
      const engines: MockEngine[] = [];
      for (const open of pages) {
        await open.emulateMedia({ colorScheme: theme, reducedMotion: theme === 'dark' ? 'reduce' : 'no-preference' });
        await open.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
        engines.push(await installMockEngine(open, { initial: { entries: [], title: 'Spanner TrueTime', sessionFile: sessionFileFor('spanner') } }));
        await installMockPlaces(open, garden());
        await hub.install(open);
        await open.goto(`/?place=${READING}`);
        await expect(open.getByRole('heading', { name: 'Reading', exact: true })).toBeVisible();
        await expect(open.getByRole('tab', { name: 'Spanner TrueTime', exact: true })).toBeVisible();
        await expect(open.getByRole('tab', { name: 'raft.github.io', exact: true })).toBeVisible();
        await expect(open).toHaveTitle('codeaf');
      }
      const [a, b] = pages;
      const mac = await a.evaluate(() => /Mac/.test(navigator.platform));
      const mod = mac ? 'Meta' : 'Control';
      await a.getByRole('tab', { name: 'Spanner TrueTime', exact: true }).click();
      await expect(b.locator('.home-tab-select')).toHaveAttribute('aria-selected', 'true');
      const composer = a.getByRole('textbox', { name: 'Message', exact: true });
      await expect(composer).toBeVisible();
      await composer.fill('one composer state');
      await expect.poll(() => JSON.stringify(hub.docs.get(READING)?.workspace ?? {}), { timeout: 10_000 }).toContain('one composer state');
      await b.getByRole('tab', { name: 'Spanner TrueTime', exact: true }).click();
      await expect(b.getByRole('textbox', { name: 'Message', exact: true })).toHaveValue('one composer state');
      await a.keyboard.press(`${mod}+t`);
      await expect(a.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
      await expect(b.getByRole('tab', { name: 'New tab', exact: true })).toBeVisible();
      await expect(b.getByRole('tab', { name: 'Spanner TrueTime', exact: true })).toHaveAttribute('aria-selected', 'true');
      await a.getByRole('button', { name: 'Close New tab', exact: true }).click();
      await expect(a.getByRole('tab', { name: 'New tab', exact: true })).toHaveCount(0);
      await expect(b.getByRole('tab', { name: 'New tab', exact: true })).toHaveCount(0);
      await expect(b.getByRole('tab', { name: 'Spanner TrueTime', exact: true })).toHaveAttribute('aria-selected', 'true');
      expect(engines.flatMap(stops)).toEqual([]);
    } finally {
      await Promise.all(contexts.map(context => context.close()));
    }
  }
});

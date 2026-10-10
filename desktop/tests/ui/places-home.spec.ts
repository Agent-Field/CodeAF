import { test, expect, type Locator, type Page } from '@playwright/test';
import { installMockEngine, type Scenario } from './support/mock-engine';
import { installMockPlaces, sessionFileFor, type MockPlaces, type PlacesSeed } from './support/mock-places';

// Places 8a–8f and 9b: Home anatomy, rolled-up attention, tiles, the composer, All places,
// first launch and inline create. Measurements come from getComputedStyle and getBoundingClientRect.

const NEW_CHAT = 'mock-1';
const EMPTY = 'Nothing here yet. Start a chat below, drag chats in from anywhere, or drop in what this place should know.';
const FIRST = 'Places hold work that belongs together, with what the AI should know about it. Open a folder or repo to make one, or just name one.';
const WIDTHS = [1200, 850, 600, 320] as const;

const fresh = (): Scenario => ({
  initial: { entries: [], title: '', sessionFile: sessionFileFor(NEW_CHAT) },
  turns: [{ entries: [{ Role: 'assistant', Text: 'On it.', Answer: true } as never] }],
});

/** The drawing's places: a parent with a child that needs you, an empty grandchild, and two loose chats. */
const garden = (): PlacesSeed => ({
  places: [
    { name: 'codeaf', tint: 'tide', pinned: true },
    { name: 'Marketing', tint: 'rose', parents: ['codeaf'] },
    { name: 'Launch site', parents: ['Marketing'] },
    { name: 'Config parser', parents: ['codeaf'], lastOpenedAt: 'now' },
    { name: 'Reading', tint: 'iris', lastOpenedAt: 'now' },
  ],
  chats: [
    { id: 'sess-need', title: 'Port fix to v1 branch', places: ['Config parser'], needsYou: true },
    { id: 'sess-run', title: 'Update fixtures', places: ['Config parser'], live: true, tasks: { running: 1 }, runningMinutes: 2 },
    { id: 'sess-loose-1', title: 'Set up the repo' },
    { id: 'sess-loose-2', title: 'What is codeaf?' },
  ],
  live: [NEW_CHAT],
});

const rail = (page: Page) => page.locator('.app-shell .place-rail');
const homeTab = (page: Page) => page.locator('.workspace-tab.is-place-home');
const composer = (page: Page) => page.locator('.home-composer').getByRole('textbox', { name: 'Message' });
const placeTiles = (page: Page) => page.locator('.places-tile[data-mode="place"] [data-places-tile-focusable]');
const tileNamed = (page: Page, name: string) => page.locator('.places-tile[data-mode="place"]').filter({ has: page.locator('.places-tile-name', { hasText: new RegExp(`^${name}$`) }) }).locator('[data-places-tile-focusable]');

type Rig = { places: MockPlaces; mod: 'Meta' | 'Control' };

async function paint(page: Page, value: string) {
  return page.evaluate(css => {
    const probe = document.createElement('span');
    probe.style.color = css;
    document.body.append(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  }, value);
}

async function boot(page: Page, theme: 'light' | 'dark', seed: PlacesSeed = garden()): Promise<Rig> {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, fresh());
  const places = await installMockPlaces(page, seed);
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
  await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toBeVisible();
  const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
  return { places, mod: mac ? 'Meta' : 'Control' };
}

async function go(page: Page, name: string) {
  await rail(page).getByRole('button', { name: new RegExp(`^${name}\\b`) }).click();
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
}

async function ring(locator: Locator) {
  const shadow = await locator.evaluate(el => getComputedStyle(el).boxShadow);
  expect(shadow).toContain('0px 0px 0px 2px');
  expect(shadow).toContain('0px 0px 0px 4px');
}

for (const theme of ['light', 'dark'] as const) {
  test(`PL-130 PL-134 PL-157 PL-259 ${theme}: place Home anatomy, attention and column`, async ({ page }) => {
    test.setTimeout(90_000);
    await boot(page, theme);
    await go(page, 'codeaf');

    const home = page.locator('.home-page');
    await expect(home.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button', { name: 'All places' })).toHaveAttribute('aria-current', 'page');
    const title = home.getByRole('heading', { level: 1, name: 'codeaf' });
    await expect(title).toHaveCSS('font-size', '28px');
    await expect(title).toHaveCSS('color', await paint(page, 'var(--ink)'));
    const swatch = home.locator('.places-heading .place-swatch[data-role="title"]');
    const swatchBox = await swatch.boundingBox();
    expect(Math.round(swatchBox?.width ?? 0)).toBe(16);
    expect(Math.round(swatchBox?.height ?? 0)).toBe(16);
    await expect(home.getByRole('button', { name: 'Place actions' })).toBeVisible();
    for (const label of await home.locator('h2.type-section-label').all()) await expect(label).toHaveCSS('font-size', '11px');

    const live = home.getByRole('region', { name: 'Live' });
    await expect(live.getByRole('list', { name: 'Live work' })).toBeVisible();
    const rows = live.locator('.home-live-list > li');
    await expect(rows.first()).toHaveAttribute('data-status', 'waiting');
    const waiting = live.locator('li[data-status="waiting"] button');
    const running = live.locator('li[data-status="running"] button');
    await expect(waiting).toHaveAccessibleName(/Needs you[\s\S]*Port fix to v1 branch[\s\S]*in Config parser[\s\S]*needs you/);
    await expect(running).toHaveAccessibleName(/Running[\s\S]*Update fixtures[\s\S]*in Config parser[\s\S]*running · 2m/);
    await expect(waiting.locator('.home-live-aside')).toHaveCSS('color', await paint(page, 'var(--ink-3)'));
    await expect(waiting.locator('.home-live-title')).toHaveCSS('color', await paint(page, 'var(--ink)'));
    // The one live shimmer clips an ink gradient through transparent text. It is not a status colour.
    await expect(running.locator('.home-live-title')).toHaveCSS('color', 'rgba(0, 0, 0, 0)');
    const mark = waiting.locator('.status-mark');
    await expect(mark).toHaveCSS('color', await paint(page, 'var(--amber)'));
    await expect(mark).toHaveCSS('color', await paint(page, 'var(--mark-waiting)'));
    const dot = await waiting.locator('.status-mark-dot').boundingBox();
    expect(Math.round(dot?.width ?? 0)).toBe(6);
    expect(Math.round(dot?.height ?? 0)).toBe(6);
    await expect(home.locator('.ui-live-shimmer')).toHaveCount(1);
    await expect(waiting.locator('.ui-live-shimmer')).toHaveCount(0);
    await expect(placeTiles(page).filter({ hasText: 'Marketing' })).toContainText('1 place');
    await expect(placeTiles(page).filter({ hasText: 'Config parser' })).toContainText('2 chats');

    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      const measured = await page.evaluate(() => {
        const column = document.querySelector('.home-column') as HTMLElement;
        const scroll = document.querySelector('.home-scroll') as HTMLElement;
        const frame = document.querySelector('.home-page') as HTMLElement;
        const dock = document.querySelector('.home-composer') as HTMLElement;
        const style = getComputedStyle(column);
        const fade = getComputedStyle(scroll);
        const before = dock.getBoundingClientRect().top;
        scroll.scrollTop = scroll.scrollHeight;
        const after = dock.getBoundingClientRect().top;
        return {
          width: column.getBoundingClientRect().width,
          scroll: scroll.clientWidth,
          pad: style.paddingLeft,
          max: style.maxWidth,
          mask: `${fade.getPropertyValue('mask-image')} ${fade.getPropertyValue('-webkit-mask-image')}`,
          docked: !scroll.contains(dock) && Math.abs(dock.getBoundingClientRect().bottom - frame.getBoundingClientRect().bottom) < 2,
          still: Math.abs(before - after) < 1,
          overflow: frame.scrollWidth - frame.clientWidth,
        };
      });
      expect(measured.pad).toBe(width === 1200 ? '24px' : '16px');
      expect(measured.mask).toContain('linear-gradient');
      expect(measured.docked).toBe(true);
      expect(measured.still).toBe(true);
      expect(measured.overflow).toBe(0);
      if (width === 1200) {
        expect(Math.round(measured.width)).toBe(680);
        expect(measured.max).toBe('680px');
      } else {
        expect(measured.max).toBe('none');
        expect(Math.abs(measured.width - measured.scroll)).toBeLessThan(2);
      }
    }

    await page.setViewportSize({ width: 1200, height: 800 });
    const tiles = placeTiles(page);
    await tiles.first().focus();
    await page.keyboard.press('ArrowRight');
    await expect(tiles.nth(1)).toBeFocused();
    await expect(page.locator('html')).not.toHaveAttribute('data-input', 'pointer');
    await ring(tiles.nth(1));
    await home.locator('h2.type-section-label', { hasText: 'Places' }).click();
    await tiles.nth(1).focus();
    await expect(page.locator('html')).toHaveAttribute('data-input', 'pointer');
    expect(await tiles.nth(1).evaluate(el => getComputedStyle(el).boxShadow)).toBe('none');

    await waiting.click();
    const opened = page.getByRole('tab', { name: 'Port fix to v1 branch' });
    await expect(opened).toHaveAttribute('aria-selected', 'true');
  });

  test(`PL-141 ${theme}: tile click, keys, Quick Look and same-window new window`, async ({ page }) => {
    const rig = await boot(page, theme);
    await go(page, 'codeaf');
    const tiles = placeTiles(page);
    const marketing = tileNamed(page, 'Marketing');
    const before = await paint(page, 'var(--surface)');
    await marketing.hover();
    await expect.poll(() => marketing.locator('xpath=ancestor::*[contains(@class,"places-tile")][1]').evaluate(el => getComputedStyle(el).backgroundColor)).toBe(await paint(page, 'var(--field)'));
    expect(before).not.toBe(await paint(page, 'var(--field)'));

    await marketing.click();
    await expect(page.getByRole('heading', { level: 1, name: 'Marketing' })).toBeVisible();
    await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button', { name: 'codeaf' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'codeaf' })).toBeVisible();

    await page.getByRole('button', { name: 'Place actions' }).focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('menuitem', { name: /Open in new window/ })).toBeVisible();
    await page.keyboard.press('Escape');

    await tiles.nth(0).focus();
    await expect(tiles.nth(0)).toBeFocused();
    const from = (await tiles.nth(0).locator('.places-tile-name').innerText()).trim();
    await page.keyboard.press('ArrowRight');
    await expect(tiles.nth(1)).toBeFocused();
    const to = (await tiles.nth(1).locator('.places-tile-name').innerText()).trim();
    expect(to).not.toBe(from);
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { level: 1, name: to, exact: true })).toBeVisible();
    expect(page.context().pages()).toHaveLength(1);
    await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button', { name: 'codeaf' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'codeaf' })).toBeVisible();

    const parser = tileNamed(page, to);
    await parser.focus();
    await expect(parser).toBeFocused();
    await page.keyboard.press('Space');
    const look = page.getByRole('dialog', { name: `Quick Look: ${to}` });
    await expect(look).toBeVisible();
    await expect(look.getByText('Space to close')).toBeVisible();
    await expect(look.getByRole('button', { name: `Go to ${to}` })).toBeVisible();
    await expect(look.getByRole('button', { name: 'Open in new window' })).toBeVisible();
    await look.press('Space');
    await expect(look).toBeHidden();
    await expect(page.getByRole('heading', { level: 1, name: 'codeaf' })).toBeVisible();

    // A browser has no second window, so the chord goes to the place here. The menu still offers the window.
    await marketing.focus();
    await expect(marketing).toBeFocused();
    await page.keyboard.press(`${rig.mod}+Enter`);
    await expect(page.getByRole('heading', { level: 1, name: 'Marketing' })).toBeVisible();
    expect(page.context().pages()).toHaveLength(1);
  });

  test(`PL-145 PL-146 PL-148 ${theme}: composer and the empty place`, async ({ page }) => {
    test.setTimeout(90_000);
    await boot(page, theme);
    await go(page, 'codeaf');
    await placeTiles(page).filter({ hasText: 'Marketing' }).click();
    await placeTiles(page).filter({ hasText: 'Launch site' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Launch site' })).toBeVisible();
    const crumbs = page.getByRole('navigation', { name: 'Breadcrumb' });
    await expect(crumbs.getByRole('button', { name: 'All places' })).toBeVisible();
    await expect(crumbs.getByRole('button', { name: 'codeaf' })).toBeVisible();
    await expect(crumbs.getByRole('button', { name: 'Marketing' })).toHaveAttribute('aria-current', 'page');
    await expect(page.getByRole('region', { name: 'Empty place' })).toContainText(EMPTY);
    await expect(composer(page)).toHaveAttribute('placeholder', 'Start the first chat in Launch site');
    const model = page.locator('.home-composer').getByRole('button', { name: 'Model: DeepSeek v4.1 Flash' });
    await expect(model).toBeVisible();
    await expect(model).toContainText('DS Flash');

    await go(page, 'codeaf');
    await expect(composer(page)).toHaveAttribute('placeholder', 'Start something in codeaf');
    await composer(page).fill('Draft the launch email');
    await composer(page).press('Enter');
    await expect(page.getByRole('tab', { name: 'Draft the launch email' })).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('.workspace-tabstrip').getByRole('tab').nth(1)).toHaveAccessibleName('Draft the launch email');
    await expect(homeTab(page)).toHaveCount(1);

    await homeTab(page).getByRole('tab').click();
    await composer(page).fill('Second thought, in the background');
    await composer(page).press(`${(await page.evaluate(() => /Mac/.test(navigator.platform))) ? 'Meta' : 'Control'}+Enter`);
    await expect(homeTab(page).getByRole('tab')).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('.workspace-tab[data-kind="conversation"]')).toHaveCount(2);
  });

  test(`PL-170 PL-173 PL-259 ${theme}: All places is the graphite root Home`, async ({ page }) => {
    const rig = await boot(page, theme);
    await page.keyboard.press(`${rig.mod}+Shift+KeyP`);
    await expect(page.getByRole('heading', { level: 1, name: 'All places' })).toBeVisible();
    await expect(page.getByRole('tab', { name: 'All places' })).toHaveAttribute('aria-selected', 'true');
    await expect(rail(page).getByRole('button', { name: /^All places/ })).toHaveAttribute('aria-current', 'page');
    await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).not.toHaveAttribute('aria-current', 'page');
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
    await expect(page.locator('.app-shell')).toHaveAttribute('data-tint', 'graphite');
    const frame = await page.locator('body').evaluate(el => {
      const style = getComputedStyle(el);
      return { h: style.getPropertyValue('--h').trim(), a: style.getPropertyValue('--a').trim() };
    });
    expect(Number(frame.h)).toBeCloseTo(250, 0);
    expect(Number(frame.a)).toBeCloseTo(0.012, 3);
    await expect(page.getByRole('searchbox', { name: 'Search places' })).toHaveAttribute('placeholder', 'Search places');
    await expect(page.getByRole('heading', { name: 'Not in any place · 2' })).toBeVisible();
    await expect(page.getByRole('button', { name: /Set up the repo/ })).toBeVisible();
    await expect(page.getByRole('button', { name: /What is codeaf\?/ })).toBeVisible();
    await expect(page.locator('.home-composer')).toHaveCount(0);
  });

  test(`PL-177 PL-259 ${theme}: first launch names a place and lists loose chats`, async ({ page }) => {
    const rig = await boot(page, theme, {
      chats: [
        { id: 'sess-loose-1', title: 'Set up the repo' },
        { id: 'sess-loose-2', title: 'What is codeaf?' },
      ],
      live: [NEW_CHAT],
    });
    await expect(rail(page).getByRole('button', { name: 'Now', exact: true })).toBeVisible();
    await expect(rail(page).getByText('Pinned', { exact: true })).toHaveCount(0);
    await expect(rail(page).getByText('Open', { exact: true })).toHaveCount(0);
    await page.keyboard.press(`${rig.mod}+Shift+KeyP`);
    await expect(page.getByRole('heading', { level: 1, name: 'All places' })).toBeVisible();
    await expect(page.locator('body')).toHaveAttribute('data-tint', 'graphite');
    await expect(rail(page).getByRole('button', { name: /^All places/ })).toHaveAttribute('aria-current', 'page');
    await expect(page.getByRole('banner', { name: 'First launch' }).or(page.locator('[aria-label="First launch"]'))).toBeVisible();
    await expect(page.getByText(FIRST)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Name a place' })).toBeVisible();
    // A browser has no folder chooser, so the tile is absent. The sentence above still says the words.
    await expect(page.getByRole('button', { name: 'Open a folder or repo' })).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Not in any place · 2' })).toBeVisible();
    await expect(page.getByRole('button', { name: /Set up the repo/ })).toBeVisible();
    await expect(page.getByRole('button', { name: /What is codeaf\?/ })).toBeVisible();
  });

  test(`PL-152 PL-180 PL-181 ${theme}: inline create refuses an empty name and opens at the top of Open`, async ({ page }) => {
    const rig = await boot(page, theme);
    await page.keyboard.press(`${rig.mod}+Shift+KeyP`);
    await expect(page.getByRole('heading', { level: 1, name: 'All places' })).toBeVisible();
    const newer = page.getByRole('button', { name: 'New place', exact: true });
    await newer.focus();
    await page.keyboard.press('Enter');
    const tint = page.getByRole('radiogroup', { name: 'Tint' });
    const radios = tint.getByRole('radio');
    await expect(radios).toHaveCount(5);
    for (const [index, label] of ['Tide', 'Rose', 'Sage', 'Sand', 'Iris'].entries()) {
      await expect(radios.nth(index)).toHaveAccessibleName(label);
    }
    await expect(page.getByText('↵ create · Esc cancel')).toBeVisible();
    const field = page.getByRole('textbox', { name: 'Place name' });
    const before = rig.places.posts('/places').length;
    await field.press('Enter');
    await field.fill('   ');
    await field.press('Enter');
    await expect.poll(() => rig.places.posts('/places').length).toBe(before);
    await page.keyboard.press('Escape');
    await expect(newer).toBeFocused();

    await page.keyboard.press('Enter');
    await page.getByRole('textbox', { name: 'Place name' }).fill('Garden');
    await page.keyboard.press('Enter');
    await expect.poll(() => rig.places.posts('/places').length).toBe(before + 1);
    expect(rig.places.posts('/places').at(-1)?.body).toMatchObject({ name: 'Garden' });
    await expect(page.getByText('Created “Garden”')).toBeVisible();
    await placeTiles(page).filter({ hasText: 'Garden' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Garden' })).toBeVisible();
    await expect(page.locator('.workspace-tabstrip').getByRole('tab')).toHaveCount(1);
    await expect(homeTab(page)).toContainText('Garden');
    const open = rail(page).getByRole('region', { name: 'Open', exact: true }).locator('.rail-rows');
    await expect(open.getByRole('button').first()).toHaveAccessibleName(/^Garden/);
  });
}

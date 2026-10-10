import { test, expect, type Page } from '@playwright/test';
import { INK3_TEXT, expectAccessible } from '../ui/contracts';

INK3_TEXT.push('.status-line', '.knows-source', '.home-specimen-composer', '.home-specimen-log', '.places-crumb', '.type-section-label', '.home-live-aside', '.home-live-detail', '.decided-sub', '.decided-age');

const now = '2026-09-26T12:00:00Z';
const knowledge = () => ({ revision: 1, stillTrue: [], lines: [{ id: 'k1', placeId: 'pl_codeaf', text: 'Use plain language', source: { kind: 'you-wrote' }, createdAt: now }] });

async function engine(page: Page, mode = 'deciding', empty = false) {
  let knows = knowledge();
  if (empty) knows.lines = [];
  const requests: string[] = [];
  await page.route('**/api/engine/**', async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    requests.push(`${request.method()} ${path}`);
    let body: unknown;
    if (path.endsWith('/decide-status')) body = { mode: empty ? 'none' : mode, agreedWeek: 41, totalWeek: 44, learning: { agreed: 14, of: 20 } };
    else if (path.endsWith('/decisions')) body = { decisions: empty ? [] : [{ id: 'd1', action: 'Allowed reading the release notes', because: 'You allowed it twice', byName: 'codeaf', percent: 97, at: now }] };
    else if (path.endsWith('/knows') && request.method() === 'GET') body = knows;
    else if (path.endsWith('/knows') && request.method() === 'POST') {
      const text = request.postDataJSON().text;
      knows = { ...knows, revision: knows.revision + 1, lines: [...knows.lines, { ...knows.lines[0], id: 'k2', text, source: { kind: 'you-wrote' }, createdAt: now, placeId: 'pl_codeaf' }] };
      body = { revision: knows.revision, noop: false, receipts: [{ id: 'r1', action: 'knows-add', beforeRevision: knows.revision - 1, afterRevision: knows.revision }], undo: ['r1'] };
    } else { await route.fulfill({ status: 404, json: { error: 'Unknown read' } }); return; }
    await route.fulfill({ json: body });
  });
  return requests;
}
async function open(page: Page, scenario = 'place', theme = 'light') {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto(`/?scenario=${scenario}`);
  await page.getByTestId('home-specimen').waitFor();
}

for (const theme of ['light', 'dark']) {
  test(`${theme}: 12a sections follow the measured design order`, async ({ page }) => {
    await engine(page);
    await open(page, 'place', theme);
    await expect(page.locator('.status-line')).toHaveText('Deciding automatically · 41 of 44 agreed this week');
    await expect(page.getByRole('region', { name: 'Decided automatically' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'What codeaf knows' })).toBeVisible();
    await expect(page.locator('.home-column')).toHaveCSS('max-width', '680px');
    await expect(page.locator('.home-column')).toHaveCSS('padding-top', '32px');
    await expect(page.locator('.home-column')).toHaveCSS('row-gap', '24px');
    await expect(page.locator('.home-heading-stack')).toHaveCSS('row-gap', '10px');
    await expect(page.locator('.status-line')).toHaveCSS('font-size', '12px');
    const selectors = ['.places-crumbs', '.places-heading-row', '.status-line', '.home-recap', '.home-live', '.decided', '.knows-heading', '.knows-add', '.home-composer'];
    const positions = await Promise.all(selectors.map(selector => page.locator(selector).evaluate(el => el.getBoundingClientRect().top)));
    expect(positions).toEqual([...positions].sort((a, b) => a - b));
    await expect(page.locator('.home-places, .home-chats, .home-sources, .home-suggestion')).toHaveCount(0);
    await page.locator('[data-decided-id="d1"] button').click();
    await expect(page.getByRole('dialog', { name: 'Why?' })).toContainText('97%');
    await expect(page.getByRole('dialog', { name: 'Why?' })).toContainText('codeaf');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog', { name: 'Why?' })).toHaveCount(0);
    await expect(page.locator('.home-specimen-composer')).toHaveText('Start something in codeaf');
  });

  test(`${theme}: learning has only a status line, never proposals on Home`, async ({ page }) => {
    await engine(page, 'learning');
    await open(page, 'place', theme);
    await expect(page.locator('.status-line')).toHaveText('Learning · 14 of 20 agreed');
    await expect(page.locator('.home-suggestion, [data-proposal-id]')).toHaveCount(0);
  });

  test(`${theme}: empty place keeps its sentence, actions and inherited context`, async ({ page }) => {
    await engine(page, 'none', true);
    await open(page, 'empty', theme);
    await expect(page.getByRole('region', { name: 'Empty place' })).toBeVisible();
    await expect(page.locator('.home-empty-sentence')).toHaveCSS('font-size', '15px');
    await expect(page.getByRole('button', { name: 'Add files or links' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Write instructions' })).toBeVisible();
    await expect(page.locator('.home-context')).toHaveText('Uses Marketing’s context: brand-voice.md, codeaf.dev');
    await expect(page.locator('.status-line, .decided, .knows-list')).toHaveCount(0);
  });

  test(`${theme}: root Home retains search, tiles, unplaced chats and offers`, async ({ page }) => {
    const requests = await engine(page);
    await open(page, 'root', theme);
    await expect(page.getByTestId('all-places-count')).toHaveText('5 at the top level · 58 in all');
    await expect(page.locator('.home-column')).toHaveCSS('row-gap', '28px');
    await expect(page.locator('.places-tile')).toHaveCount(6);
    await expect(page.locator('.home-suggestion')).toContainText('5 of these look like they belong in Reading');
    await expect(page.getByRole('heading', { name: /Not in any place · 38/ })).toBeVisible();
    await page.getByRole('searchbox', { name: 'Search places' }).fill('papers');
    await expect(page.locator('[data-place-id="pl_papers"]')).toContainText('in Reading');
    expect(requests).toEqual([]);
  });

  test(`${theme}: knowledge saves through the bridge and refreshes`, async ({ page }) => {
    const requests = await engine(page);
    await open(page, 'place', theme);
    const add = page.getByRole('textbox', { name: 'Add something codeaf should know' });
    await add.fill('Keep headings short');
    await add.press('Enter');
    await expect(page.locator('.knows-text').filter({ hasText: 'Keep headings short' })).toBeVisible();
    await expect(add).toHaveValue('');
    expect(requests).toContain('POST /api/engine/places/pl_codeaf/knows');
  });
}

test('a failed section read leaves available data visible and Retry reads again', async ({ page }) => {
  await engine(page);
  await page.route('**/decide-status', route => route.fulfill({ status: 503, json: { error: 'Status unavailable' } }));
  await open(page);
  await expect(page.getByRole('region', { name: 'What codeaf knows' })).toBeVisible();
  await expect(page.locator('.status-line')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Retry Home sections' })).toBeVisible();
  await page.unroute('**/decide-status');
  await page.getByRole('button', { name: 'Retry Home sections' }).click();
  await expect(page.locator('.status-line')).toBeVisible();
});

test('Now and offline retain their existing read-only behavior', async ({ page }) => {
  await engine(page);
  await open(page, 'now');
  await expect(page.getByRole('heading', { level: 1, name: 'Now' })).toBeVisible();
  await expect(page.locator('.status-line, .decided, .knows-list')).toHaveCount(0);
  await open(page, 'offline');
  await expect(page.getByRole('status')).toContainText('changes are paused');
  await expect(page.locator('.knows-add')).toHaveCount(0);
});

for (const width of [320, 480, 600, 800, 1200]) {
  test(`12a fits at ${width}px in both appearances`, async ({ page }) => {
    await engine(page);
    await page.setViewportSize({ width, height: 700 });
    for (const theme of ['light', 'dark']) {
      await open(page, 'place', theme);
      await expect(page.locator('.knows-add')).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0);
      expect(await page.locator('.home-column').evaluate(el => el.scrollWidth - el.clientWidth)).toBe(0);
    }
  });
}

const log = (page: Page) => page.getByRole('list', { name: 'Callback log' }).locator('li');
const tile = (page: Page, id: string) => page.locator(`[data-place-id="${id}"]`);

for (const theme of ['light', 'dark'] as const) {
    test(`${theme}: empty place actions and breadcrumb`, async ({ page }) => {
      await engine(page, 'none', true);
      await open(page, 'empty', theme);
      await expect(page.getByText('Nothing here yet. Start a chat below, drag chats in from anywhere, or drop in what this place should know.')).toBeVisible();
      await expect(page.locator('.home-empty-sentence')).toHaveCSS('font-size', '15px');
      await expect(page.locator('.home-empty-sentence')).toHaveCSS('max-width', '480px');
      await expect(page.getByRole('button', { name: 'Add files or links' })).toBeVisible();
      await expect(page.getByRole('button', { name: 'Write instructions' })).toBeVisible();
      await expect(page.getByText('Uses Marketing’s context: brand-voice.md, codeaf.dev')).toBeVisible();
      await expect(page.locator('.places-tile')).toHaveCount(0);
      await expect(page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button')).toHaveText(['All places', 'codeaf', 'Marketing']);
      await page.getByRole('button', { name: 'Add files or links' }).click();
      await expect(log(page).last()).toHaveText('addSources:pl_launch');
      await page.getByRole('button', { name: 'Write instructions' }).click();
      await expect(log(page).last()).toHaveText('writeInstructions:pl_launch');
      await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button', { name: 'Marketing' }).click();
      await expect(log(page).last()).toHaveText('goTo:pl_marketing');
    });

    test(`${theme}: All places search, create, archive and restore`, async ({ page }) => {
      await open(page, 'root', theme);
      await expect(page.getByRole('heading', { level: 1, name: 'All places' })).toHaveCSS('font-size', '28px');
      await expect(page.getByTestId('all-places-count')).toHaveText('5 at the top level · 58 in all');
      await expect(page.locator('.home-places .places-tile')).toHaveCount(6);
      await expect(page.getByRole('heading', { name: /Not in any place · 38/ })).toBeVisible();
      await expect(page.locator('.home-suggestion')).toContainText('5 of these look like they belong in Reading');
      await page.getByRole('button', { name: 'Move them' }).click();
      await expect(log(page).last()).toHaveText('acceptSuggestion');
      // Search reaches nested places and names where they are.
      const search = page.getByRole('searchbox', { name: 'Search places' });
      await expect(search).toHaveCSS('height', '30px');
      await search.fill('papers');
      await expect(tile(page, 'pl_papers')).toContainText('in Reading');
      await expect(page.locator('.home-places .places-tile')).toHaveCount(1);
      await expect(page.getByRole('heading', { name: /Not in any place/ })).toHaveCount(0);
      await search.fill('zzz');
      await expect(page.getByText('No place called “zzz”.')).toBeVisible();
      await page.getByRole('button', { name: 'Create “zzz”' }).click();
      await expect(log(page).last()).toHaveText('create:zzz:none:root');
      await expect(search).toHaveValue('');
      await search.fill('soft');
      await search.press('Escape');
      await expect(search).toHaveValue('');
      // Archived places sit behind one toggle, and restore from their menu.
      const toggle = page.getByRole('button', { name: /Archived · 1/ });
      await expect(toggle).toHaveAttribute('aria-expanded', 'false');
      await toggle.click();
      await expect(toggle).toHaveAttribute('aria-expanded', 'true');
      await tile(page, 'pl_old').click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Restore' }).click();
      await expect(log(page).last()).toHaveText('restore:pl_old');
    });

    test(`${theme}: first launch keeps folder and new place actions`, async ({ page }) => {
      await open(page, 'first', theme);
      await expect(page.getByText('Places hold work that belongs together, with what the AI should know about it.')).toBeVisible();
      await expect(page.getByRole('searchbox')).toHaveCount(0);
      await expect(page.getByTestId('all-places-count')).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Name a place' })).toBeVisible();
      await page.getByRole('button', { name: 'Open a folder or repo' }).click();
      await expect(log(page).last()).toHaveText('openFolder');
      await expect(page.getByRole('heading', { name: /Not in any place · 2/ })).toBeVisible();
    });


}

for (const theme of ['light', 'dark']) {
  test(`${theme}: keyboard knowledge entry and accessible Home`, async ({ page }) => {
    await engine(page);
    await open(page, 'place', theme);
    await page.getByRole('textbox', { name: 'Add something codeaf should know' }).focus();
    await expect(page.getByRole('textbox', { name: 'Add something codeaf should know' })).toBeFocused();
    await expectAccessible(page);
  });
}

test('failed knowledge write preserves the draft and adds no saved line', async ({ page }) => {
  await engine(page);
  await page.route('**/knows', async route => {
    if (route.request().method() === 'GET') { await route.fallback(); return; }
    await route.fulfill({ status: 409, json: { error: 'The place changed. Try again.' } });
  });
  await open(page);
  const add = page.getByRole('textbox', { name: 'Add something codeaf should know' });
  await add.fill('Keep headings short');
  await add.press('Enter');
  await expect(add).toHaveValue('Keep headings short');
  await expect(page.locator('.knows-error')).toBeVisible();
  await expect(page.locator('.knows-text')).toHaveCount(1);
});

test('menus: Always ask me toggles and Decision confidence… writes the chosen figure', async ({ page }) => {
  // A place Home no longer draws its child tiles. All places still does, and that tile menu is the same place menu.
  await open(page, 'root');
  await tile(page, 'pl_codeaf').click({ button: 'right' });
  const ask = page.getByRole('menuitemcheckbox', { name: 'Always ask me' });
  await expect(ask).toHaveAttribute('aria-checked', 'false');
  await ask.click();
  await expect(log(page).last()).toHaveText('decide:pl_codeaf:true:');
  await tile(page, 'pl_codeaf').click({ button: 'right' });
  await expect(page.getByRole('menuitemcheckbox', { name: 'Always ask me' })).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('menuitem', { name: 'Decision confidence…' }).click();
  await expect(page.getByRole('menuitemcheckbox', { name: '90%' })).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('menuitemcheckbox', { name: '70%' }).click();
  await expect(log(page).last()).toHaveText('decide:pl_codeaf::70');
  // A place the engine did not describe draws neither entry.
  await tile(page, 'pl_reports').click({ button: 'right' });
  await expect(page.getByRole('menuitemcheckbox', { name: 'Always ask me' })).toHaveCount(0);
  await expect(page.getByRole('menuitem', { name: 'Decision confidence…' })).toHaveCount(0);
});

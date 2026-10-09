import { test, expect, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { INK3_TEXT, expectAccessible, tokenColor } from '../ui/contracts';

// The Places Home against the designer's measurements (Places 8a to 8e) and the journeys a person takes through it. Numbers are read from the
// design, not from tokens.json, so a drifting token fails here. Screenshots go outside the source tree, for review only.
INK3_TEXT.push('.home-quiet', '.home-quicklook-hint', '.all-places-search', '.home-specimen-composer', '.places-tile-hint', '.home-archived-toggle', '.home-notice',
  '.places-row-aside', '.places-row-muted', '.places-chat-excerpt', '.places-chat-time', '.places-tile-meta', '.places-crumb', '.type-section-label', '.places-heading-menu', '.places-chat-lead', '.places-tile-main', '.home-specimen-log');
const shots = process.env.PLACES_HOME_SHOTS ?? '/home/santosh/.codex/codeaf-design-run/places-home-shots';
mkdirSync(shots, { recursive: true });

type Scenario = 'place' | 'empty' | 'root' | 'first' | 'now' | 'loading' | 'error' | 'offline';
async function open(page: Page, scenario: Scenario, theme: 'light' | 'dark' = 'light', extra = '') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto(`/?scenario=${scenario}${extra}`);
  await page.getByTestId('home-specimen').waitFor();
}
const log = (page: Page) => page.getByRole('list', { name: 'Callback log' }).locator('li');
const tile = (page: Page, id: string) => page.locator(`[data-place-id="${id}"]`);
const chat = (page: Page, id: string) => page.locator(`[data-chat-id="${id}"]`);
const attention = (page: Page, id: string) => page.locator(`[data-attention-id="${id}"]`);
const primary = process.platform === 'darwin' ? 'Meta' : 'Control';
const shot = (page: Page, name: string, theme: string) => page.screenshot({ path: `${shots}/${name}-${theme}-${test.info().project.name}.png` });

for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme}`, () => {
    test('place Home (8a): column, title, recap, attention, tiles, chats, composer', async ({ page }) => {
      await open(page, theme);
      const column = page.locator('.home-column');
      await expect(column).toHaveCSS('max-width', '680px');
      await expect(column).toHaveCSS('padding-top', '40px');
      await expect(column).toHaveCSS('padding-left', '24px');
      await expect(column).toHaveCSS('row-gap', '28px');
      const title = page.getByRole('heading', { level: 1, name: 'codeaf' });
      await expect(title).toHaveCSS('font-size', '28px');
      await expect(title).toHaveCSS('font-weight', '600');
      await expect(page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button', { name: 'All places' })).toBeVisible();
      await expect(page.locator('.places-heading .place-swatch')).toHaveCSS('width', '16px');
      // Recap: 11px label, then 14px prose at 1.6.
      const recap = page.getByRole('region', { name: 'Since yesterday' });
      await expect(recap.locator('.type-section-label')).toHaveCSS('font-size', '11px');
      await expect(recap.locator('.home-recap-text')).toHaveCSS('font-size', '14px');
      await expect(recap.locator('.home-recap-text')).toContainText('Trailing commas ship outside strict mode');
      // Attention: 36px rows, two of them, the place named in muted words.
      await expect(attention(page, 'chat_port')).toHaveCSS('height', '36px');
      await expect(attention(page, 'chat_port')).toContainText('Port fix to v1 branch · in Config parser');
      await expect(attention(page, 'chat_fixtures').locator('.places-row-aside')).toHaveText('running · 2m');
      // Tiles: 84px, three plus the New place tile, a status dot only on the one that needs you.
      await expect(tile(page, 'pl_software')).toHaveCSS('height', '84px');
      await expect(page.locator('.places-home, .home-places .places-tile')).toHaveCount(4);
      await expect(tile(page, 'pl_software').locator('.status-mark')).toHaveCount(1);
      await expect(tile(page, 'pl_marketing').locator('.status-mark')).toHaveCount(0);
      await expect(tile(page, 'pl_release')).toContainText('6 chats · also in Software');
      // Chats: three 44px rows, the running one carries the dot, times are short and relative to the injected clock.
      await expect(chat(page, 'chat_commas')).toHaveCSS('height', '44px');
      await expect(chat(page, 'chat_commas').locator('.places-chat-time')).toHaveText('1h');
      await expect(chat(page, 'chat_naming').locator('.places-chat-time')).toHaveText('Tue');
      await expect(chat(page, 'chat_pricing').locator('.places-chat-time')).toHaveText('Last wk');
      await expect(page.locator('.home-specimen-composer')).toHaveText('Start something in codeaf');
      await expectAccessible(page);
      await shot(page, 'place-home', theme);
    });

    test('empty place (8b): one sentence, two actions, the context line, no Places section', async ({ page }) => {
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
      await expectAccessible(page);
      await shot(page, 'empty-place', theme);
    });

    test('All places (8c): count line, search, archive, unplaced chats and the engine suggestion', async ({ page }) => {
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
      await expectAccessible(page);
      await shot(page, 'all-places', theme);
    });

    test('first launch (8e): one sentence, two tiles, the chats in no place', async ({ page }) => {
      await open(page, 'first', theme);
      await expect(page.getByText('Places hold work that belongs together, with what the AI should know about it.')).toBeVisible();
      await expect(page.getByRole('searchbox')).toHaveCount(0);
      await expect(page.getByTestId('all-places-count')).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Name a place' })).toBeVisible();
      await page.getByRole('button', { name: 'Open a folder or repo' }).click();
      await expect(log(page).last()).toHaveText('openFolder');
      await expect(page.getByRole('heading', { name: /Not in any place · 2/ })).toBeVisible();
      await expectAccessible(page);
      await shot(page, 'first-launch', theme);
    });

    test('Now lists chats in no place and has no places controls', async ({ page }) => {
      await open(page, 'now', theme);
      await expect(page.getByRole('heading', { level: 1, name: 'Now' })).toBeVisible();
      await expect(page.locator('.places-tile')).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Place actions' })).toHaveCount(0);
      await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveCount(0);
      await chat(page, 'chat_regex').click();
      await expect(log(page).last()).toHaveText('openChat:chat_regex');
      await expectAccessible(page);
    });

    test('loading, error and offline are states of the page', async ({ page }) => {
      await open(page, 'loading', theme);
      await expect(page.getByRole('status')).toHaveText('Loading places');
      await expect(page.locator('.places-tile')).toHaveCount(0);
      await expectAccessible(page);

      await page.goto('/?scenario=error');
      await page.getByTestId('home-specimen').waitFor();
      await expect(page.getByRole('alert')).toContainText('Could not read your places. Nothing was changed.');
      await page.getByRole('button', { name: 'Retry' }).click();
      await expect(log(page).last()).toHaveText('retry');
      await expectAccessible(page);
      await shot(page, 'error', theme);

      // Offline keeps the last good page, read-only: going and reading stay, every write is gone.
      await page.goto('/?scenario=offline');
      await page.getByTestId('home-specimen').waitFor();
      await expect(page.getByRole('status')).toContainText('Can’t reach codeaf right now. This is the last page it loaded, so changes are paused.');
      await expect(tile(page, 'pl_software')).toBeVisible();
      await expect(page.locator('.places-tile[data-mode="new"] .places-tile-main')).toBeDisabled();
      await tile(page, 'pl_software').click({ button: 'right' });
      await expect(page.getByRole('menuitem', { name: 'Go to' })).toBeVisible();
      for (const gone of ['Rename', 'Tint', 'Archive', 'Delete place…']) await expect(page.getByRole('menuitem', { name: gone })).toHaveCount(0);
      await page.keyboard.press('Escape');
      await page.getByRole('button', { name: 'Retry' }).click();
      await expect(log(page).last()).toHaveText('retry');
      await expect(tile(page, 'pl_software')).toHaveJSProperty('draggable', false);
      await shot(page, 'offline', theme);
    });
  });
}

test.describe('journeys', () => {
  test('keyboard on tiles: Enter goes, modifier Enter opens a window, Space opens Quick Look and gives focus back', async ({ page }) => {
    await open(page, 'place');
    const software = tile(page, 'pl_software').locator('.places-tile-main');
    await software.focus();
    await page.keyboard.press('Enter');
    await expect(log(page).last()).toHaveText('goTo:pl_software');
    await page.keyboard.press(`${primary}+Enter`);
    await expect(log(page).last()).toHaveText('newWindow:pl_software');
    await page.keyboard.press('ArrowRight');
    await expect(tile(page, 'pl_marketing').locator('.places-tile-main')).toBeFocused();
    await page.keyboard.press('End');
    await expect(page.locator('.places-tile[data-mode="new"] .places-tile-main')).toBeFocused();
    await page.keyboard.press('Home');
    await page.keyboard.press('Space');
    await expect(log(page).last()).toHaveText('quickLook:pl_software');
    const sheet = page.getByRole('dialog', { name: 'Quick Look: Software' });
    await expect(sheet).toBeVisible();
    await expect(sheet.getByText('Space to close')).toBeVisible();
    await expect(sheet.getByRole('button', { name: 'Go to Software' })).toBeFocused();
    await expect(tile(page, 'pl_software')).toHaveAttribute('data-selected', 'true');
    await page.keyboard.press('Space');
    await expect(sheet).toHaveCount(0);
    await expect(software).toBeFocused();
    // Quick Look does not go anywhere: the only calls so far are the ones above.
    await expect(log(page).filter({ hasText: 'goTo:' })).toHaveCount(1);
  });

  test('Quick Look: Go to leaves the sheet and goes; Escape closes; the scrim closes', async ({ page }) => {
    await open(page, 'place');
    await tile(page, 'pl_marketing').locator('.places-tile-main').focus();
    await page.keyboard.press('Space');
    const sheet = page.getByRole('dialog', { name: 'Quick Look: Marketing' });
    await page.getByRole('button', { name: 'Go to Marketing' }).click();
    await expect(sheet).toHaveCount(0);
    await expect(log(page).filter({ hasText: 'goTo:pl_marketing' })).toHaveCount(1);
    await tile(page, 'pl_marketing').locator('.places-tile-main').focus();
    await page.keyboard.press('Space');
    await page.keyboard.press('Escape');
    await expect(sheet).toHaveCount(0);
    await expect(tile(page, 'pl_marketing').locator('.places-tile-main')).toBeFocused();
    await page.keyboard.press('Space');
    await page.getByRole('button', { name: 'Open in new window' }).click();
    await expect(log(page).filter({ hasText: 'newWindow:pl_marketing' })).toHaveCount(1);
  });

  test('pointer: click goes, modifier click and middle click open elsewhere, rows open chats', async ({ page }) => {
    await open(page, 'place');
    await tile(page, 'pl_marketing').click();
    await expect(log(page).last()).toHaveText('goTo:pl_marketing');
    await tile(page, 'pl_marketing').click({ modifiers: [primary as 'Control'] });
    await expect(log(page).last()).toHaveText('newWindow:pl_marketing');
    await tile(page, 'pl_release').locator('.places-tile-main').dispatchEvent('auxclick', { button: 1 });
    await expect(log(page).last()).toHaveText('newWindow:pl_release');
    await chat(page, 'chat_naming').click();
    await expect(log(page).last()).toHaveText('openChat:chat_naming');
    await chat(page, 'chat_naming').click({ modifiers: [primary as 'Control'] });
    await expect(log(page).last()).toHaveText('openChatNewTab:chat_naming');
    await chat(page, 'chat_pricing').locator('.places-chat-main').dispatchEvent('auxclick', { button: 1 });
    await expect(log(page).last()).toHaveText('openChatNewTab:chat_pricing');
    await attention(page, 'chat_port').click();
    await expect(log(page).last()).toHaveText('openChat:chat_port');
    await attention(page, 'chat_fixtures').click({ modifiers: [primary as 'Control'] });
    await expect(log(page).last()).toHaveText('openChatNewTab:chat_fixtures');
    // Arrow keys walk the chat list.
    await chat(page, 'chat_commas').locator('.places-chat-main').focus();
    await page.keyboard.press('ArrowDown');
    await expect(chat(page, 'chat_naming').locator('.places-chat-main')).toBeFocused();
    await page.keyboard.press('ArrowUp');
    await expect(chat(page, 'chat_commas').locator('.places-chat-main')).toBeFocused();
  });

  test('up a level: the breadcrumb and the up key go to the parent', async ({ page }) => {
    await open(page, 'empty');
    await page.getByRole('button', { name: 'Add files or links' }).focus();
    await page.keyboard.press(`${primary}+[`);
    await expect(log(page).last()).toHaveText('goTo:pl_marketing');
    await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button', { name: 'All places' }).click();
    await expect(log(page).last()).toHaveText('goTo:root');
    await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('button', { name: 'codeaf' }).click({ modifiers: [primary as 'Control'] });
    await expect(log(page).last()).toHaveText('newWindow:pl_codeaf');
  });

  test('create a place inline: name, tint by arrow keys, Enter; Escape cancels; a clash and a failure say so', async ({ page }) => {
    await open(page, 'place');
    await page.getByRole('button', { name: 'New place' }).click();
    const field = page.getByRole('textbox', { name: 'Place name' });
    await expect(field).toBeFocused();
    await field.fill('Talks');
    await page.getByRole('radio', { name: 'Iris' }).click();
    await expect(page.getByRole('radio', { name: 'Iris' })).toBeChecked();
    await page.getByRole('radio', { name: 'Iris' }).press('ArrowRight');
    await expect(page.getByRole('radio', { name: 'Rose' })).toBeChecked();
    await field.press('Escape');
    await expect(field).toHaveCount(0);
    await expect(log(page)).toHaveCount(0);

    await page.getByRole('button', { name: 'New place' }).click();
    await field.fill('software');
    await expect(field).toHaveAttribute('aria-invalid', 'true');
    await expect(page.getByText('There is already a place called “software” here.')).toBeVisible();
    await field.press('Enter');
    await expect(log(page)).toHaveCount(0);

    await field.fill('Fails');
    await field.press('Enter');
    await expect(page.getByRole('alert')).toContainText('That would put “Fails” inside “Fails”.');
    await expect(field).toBeVisible();
    await page.getByRole('button', { name: 'Dismiss' }).click();

    await field.fill('Talks');
    await page.getByRole('radio', { name: 'Iris' }).click();
    await field.press('Enter');
    await expect(log(page).last()).toHaveText('create:Talks:iris:pl_codeaf');
    await expect(field).toHaveCount(0);
    await expect(tile(page, 'pl_talks')).toBeVisible();
  });

  test('menus: tint, rename in place, pin, merge and another parent entrypoints', async ({ page }) => {
    await open(page, 'place');
    await tile(page, 'pl_marketing').click({ button: 'right' });
    const items = await page.getByRole('menuitem').allTextContents();
    expect(items.map(text => text.replace(/\s+/g, ' ').trim())).toEqual(['Go to↵', 'Quick LookSpace', 'Open in new window⌘↵', 'Rename', 'Tint', 'Add to another place…', 'Merge into…', 'Pin to rail', 'Archive', 'Delete place…']);
    await page.getByRole('menuitem', { name: 'Tint' }).click();
    await page.getByRole('menuitemcheckbox', { name: 'Sage' }).or(page.getByRole('menuitem', { name: 'Sage' })).first().click();
    await expect(log(page).last()).toHaveText('tint:pl_marketing:sage');

    await tile(page, 'pl_marketing').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Rename' }).click();
    const field = page.getByRole('textbox', { name: 'Place name' });
    await expect(field).toHaveValue('Marketing');
    await field.fill('Brand');
    await field.press('Enter');
    await expect(log(page).last()).toHaveText('rename:pl_marketing:Brand');
    await expect(tile(page, 'pl_marketing')).toContainText('Brand');

    await tile(page, 'pl_release').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Merge into…' }).click();
    await expect(log(page).last()).toHaveText('chooseMerge:pl_release');
    await tile(page, 'pl_release').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Add to another place…' }).click();
    await expect(log(page).last()).toHaveText('chooseParent:pl_release');
    await tile(page, 'pl_release').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Pin to rail' }).click();
    await expect(log(page).last()).toHaveText('pin:pl_release');
    await tile(page, 'pl_release').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Archive' }).click();
    await expect(log(page).last()).toHaveText('archive:pl_release');
    await expect(tile(page, 'pl_release')).toHaveCount(0);
  });

  test('the Home menu renames the title in place, and Escape cancels', async ({ page }) => {
    await open(page, 'place');
    await page.getByRole('button', { name: 'Place actions' }).click();
    const items = await page.getByRole('menuitem').allTextContents();
    expect(items.map(text => text.replace(/\s+/g, ' ').trim())).toEqual(['Rename', 'Tint', 'Add to another place…', 'Merge into…', 'Unpin from rail', 'Archive', 'Delete place…']);
    await page.getByRole('menuitem', { name: 'Rename' }).click();
    const field = page.getByRole('textbox', { name: 'Place name' });
    await expect(field).toBeFocused();
    await expect(field).toHaveValue('codeaf');
    await field.press('Escape');
    await expect(field).toHaveCount(0);
    await expect(log(page)).toHaveCount(0);
    await page.getByRole('button', { name: 'Place actions' }).click();
    await page.getByRole('menuitem', { name: 'Rename' }).click();
    await field.fill('codeaf core');
    await field.press('Enter');
    await expect(log(page).last()).toHaveText('rename:pl_codeaf:codeaf core');
  });

  test('delete shows what would change first, and only then deletes', async ({ page }) => {
    await open(page, 'place');
    await tile(page, 'pl_release').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Delete place…' }).click();
    const confirm = page.getByRole('group', { name: 'Delete Release' });
    await expect(confirm).toContainText('Delete “Release”? 1 place inside it moves up a level; 2 chats are left in no place. No chat is deleted.');
    await expect(log(page).last()).toHaveText('preview:pl_release');
    await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused();
    await confirm.getByRole('button', { name: 'Cancel' }).click();
    await expect(confirm).toHaveCount(0);
    await expect(log(page).filter({ hasText: 'remove:' })).toHaveCount(0);
    await tile(page, 'pl_release').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Delete place…' }).click();
    await page.getByRole('button', { name: 'Delete place' }).click();
    await expect(log(page).last()).toHaveText('remove:pl_release');
    await expect(tile(page, 'pl_release')).toHaveCount(0);
  });

  test('drag and drop: a chat or a place onto a tile adds it, Alt moves it, a place never drops on itself', async ({ page }) => {
    await open(page, 'place');
    const drag = async (source: string, target: string, alt = false) => {
      const data = await page.evaluateHandle(() => new DataTransfer());
      await page.locator(source).dispatchEvent('dragstart', { dataTransfer: data });
      await page.locator(target).dispatchEvent('dragenter', { dataTransfer: data, altKey: alt });
      await page.locator(target).dispatchEvent('dragover', { dataTransfer: data, altKey: alt });
      return data;
    };
    let data = await drag('[data-chat-id="chat_naming"]', '[data-place-id="pl_marketing"]');
    await expect(chat(page, 'chat_naming')).toHaveAttribute('data-dragging', 'true');
    await expect(tile(page, 'pl_marketing')).toHaveAttribute('data-drop', 'true');
    await expect(tile(page, 'pl_marketing').locator('.places-tile-drop')).toHaveText('Add here');
    await expect(tile(page, 'pl_software')).not.toHaveAttribute('data-drop', 'true');
    await page.locator('[data-place-id="pl_marketing"]').dispatchEvent('drop', { dataTransfer: data });
    await page.locator('[data-chat-id="chat_naming"]').dispatchEvent('dragend', { dataTransfer: data });
    await expect(log(page).last()).toHaveText('file:chat:chat_naming:pl_marketing:add');
    await expect(tile(page, 'pl_marketing')).not.toHaveAttribute('data-drop', 'true');

    data = await drag('[data-chat-id="chat_pricing"]', '[data-place-id="pl_release"]', true);
    await page.locator('[data-place-id="pl_release"]').dispatchEvent('drop', { dataTransfer: data, altKey: true });
    await expect(log(page).last()).toHaveText('file:chat:chat_pricing:pl_release:move');

    data = await drag('[data-place-id="pl_release"]', '[data-place-id="pl_software"]');
    await expect(tile(page, 'pl_release')).toHaveAttribute('data-dragging', 'true');
    await page.locator('[data-place-id="pl_software"]').dispatchEvent('drop', { dataTransfer: data });
    await expect(log(page).last()).toHaveText('file:place:pl_release:pl_software:add');

    // Over itself: no highlight, and a drop does nothing.
    data = await drag('[data-place-id="pl_release"]', '[data-place-id="pl_release"]');
    await expect(tile(page, 'pl_release')).not.toHaveAttribute('data-drop', 'true');
    const before = await log(page).count();
    await page.locator('[data-place-id="pl_release"]').dispatchEvent('drop', { dataTransfer: data });
    await expect(log(page)).toHaveCount(before);
  });

  test('sources: quiet rows, a missing one says so, Remove is reachable by keyboard', async ({ page }) => {
    await open(page, 'place');
    const sources = page.getByRole('region', { name: 'Sources' });
    await expect(sources.locator('.type-section-label')).toHaveText('Sources');
    await expect(sources.locator('[data-source-id="src_notes"]')).toContainText('file · missing');
    await expect(sources.locator('[data-source-id="src_parse"]')).toContainText('codeaf/internal/parse');
    await page.getByRole('button', { name: 'Remove release-notes.md' }).focus();
    await expect(page.getByRole('button', { name: 'Remove release-notes.md' })).toBeVisible();
    await page.keyboard.press('Enter');
    await expect(log(page).last()).toHaveText('removeSource:pl_codeaf:src_notes');
    await page.goto('/?scenario=offline');
    await page.getByTestId('home-specimen').waitFor();
    await expect(page.getByRole('button', { name: /^Remove / })).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Sources' })).toBeVisible();
    await page.goto('/?scenario=empty');
    await page.getByTestId('home-specimen').waitFor();
    await expect(page.getByRole('region', { name: 'Sources' })).toHaveCount(0);
  });

  test('attention puts what needs you first, whatever order it arrives in', async ({ page }) => {
    await open(page, 'place');
    await expect(page.locator('.places-attention-row').first()).toHaveAttribute('data-attention-id', 'chat_port');
  });

  test('narrow windows keep a 16px gutter and a full-width Quick Look', async ({ page }) => {
    await page.setViewportSize({ width: 480, height: 700 });
    await open(page, 'place');
    await expect(page.locator('.home-column')).toHaveCSS('padding-left', '16px');
    await tile(page, 'pl_software').locator('.places-tile-main').focus();
    await page.keyboard.press('Space');
    const box = await page.getByRole('dialog', { name: 'Quick Look: Software' }).boundingBox();
    expect(box!.width).toBeLessThanOrEqual(480 - 32 + 1);
    expect(box!.x).toBeGreaterThanOrEqual(0);
  });

  test('chat menu removes a chat from this place and offers the place chooser', async ({ page }) => {
    await open(page, 'place');
    await chat(page, 'chat_naming').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Add to a place…' }).click();
    await expect(log(page).last()).toHaveText('chooseChatPlace:chat_naming');
    await chat(page, 'chat_naming').click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Remove from this place' }).click();
    await expect(log(page).last()).toHaveText('removeChat:chat_naming:pl_codeaf');
  });

  test('a verb the owner did not wire has no control: nothing on the page is a dead button', async ({ page }) => {
    await open(page, 'place', 'light', '&bare=1');
    await expect(page.getByRole('button', { name: 'Place actions' })).toHaveCount(0);
    await expect(page.locator('.places-tile[data-mode="new"]')).toHaveCount(0);
    await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toHaveCount(0);
    for (const id of ['pl_software', 'pl_marketing']) await expect(tile(page, id).locator('.places-tile-main')).toBeDisabled();
    await expect(chat(page, 'chat_naming').locator('.places-chat-main')).toBeDisabled();
    await expect(tile(page, 'pl_software')).toHaveJSProperty('draggable', false);
    await page.goto('/?scenario=empty&bare=1');
    await page.getByTestId('home-specimen').waitFor();
    await expect(page.getByRole('button', { name: 'Add files or links' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Write instructions' })).toHaveCount(0);
    await expect(page.getByText('Nothing here yet.')).toBeVisible();
    await page.goto('/?scenario=root&bare=1');
    await page.getByTestId('home-specimen').waitFor();
    await expect(page.getByRole('button', { name: 'Move them' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'New place' })).toHaveCount(0);
  });

  test('every scenario fits at 320, 480, 600 and 800px with no horizontal overflow', async ({ page }) => {
    for (const scenario of ['place', 'empty', 'root', 'first', 'now', 'error', 'offline'] as const) {
      for (const width of [320, 480, 600, 800]) {
        await page.setViewportSize({ width, height: 700 });
        await open(page, scenario);
        const overflow = await page.evaluate(() => ({ page: document.documentElement.scrollWidth - document.documentElement.clientWidth, column: (() => { const column = document.querySelector('.home-column')!; return column.scrollWidth - column.clientWidth; })() }));
        expect(overflow, `${scenario} at ${width}`).toEqual({ page: 0, column: 0 });
        if (scenario === 'place') await expect(page.getByRole('button', { name: 'Place actions' })).toBeVisible();
      }
    }
    await page.setViewportSize({ width: 320, height: 700 });
    await open(page, 'place');
    await shot(page, 'place-320', 'light');
    await open(page, 'root', 'dark');
    await shot(page, 'root-320', 'dark');
  });

  test('reduced motion removes the tile and row transitions', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await open(page, 'place');
    for (const selector of ['.places-tile', '.places-chat-row', '.places-attention-row']) {
      const duration = await page.locator(selector).first().evaluate(el => getComputedStyle(el).transitionDuration);
      expect(duration.split(',').every(value => value.trim() === '0s'), `${selector}: ${duration}`).toBe(true);
    }
  });

  test('Quick Look is accessible in both themes and keeps the page behind it', async ({ page }) => {
    for (const theme of ['light', 'dark'] as const) {
      await open(page, 'place', theme);
      await tile(page, 'pl_software').locator('.places-tile-main').focus();
      await page.keyboard.press('Space');
      const sheet = page.getByRole('dialog', { name: 'Quick Look: Software' });
      await expect(sheet).toBeVisible();
      await expect(sheet).toHaveCSS('border-top-left-radius', '14px');
      await expect(sheet).toHaveCSS('background-color', await tokenColor(page, 'canvas'));
      await expectAccessible(page);
      await shot(page, 'quick-look', theme);
      await page.keyboard.press('Escape');
    }
  });
});

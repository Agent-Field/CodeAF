import { test, expect, type Page } from '@playwright/test';
import { expectAccessible, tokenColor } from './contracts';
import design from '../../src/design/tokens.json' with { type: 'json' };

// The overview (design 2h, 3h grid, 3i filmstrip): measured from the design files and pinned here from tokens.
const f = design.foundation as Record<string, string>;
const px = (name: string) => parseFloat(f[name]);
const key = (page: Page, chord: string) => page.keyboard.press(chord);
test.beforeEach(async ({ page }) => { await page.route('**/api/engine/**', route => route.abort()); });

const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: `${title} draft`, pinned: false, kind: 'conversation', ...over });
async function seed(page: Page, activeId = 'a') {
  const state = {
    tabs: [
      tab('a', 'Config stack', { groupId: 'g', draft: 'I split it into two groups.' }), tab('b', 'Update fixtures', { groupId: 'g', kind: 'task' }),
      tab('c', 'Port fix', { groupId: 'g' }), tab('d', 'Lexer', { groupId: 'g' }),
      tab('e', 'Release v2.4'), tab('f', 'Models', { draft: '' }),
    ],
    groups: [{ id: 'g', title: 'Trailing commas', collapsed: false }], closed: [], activeId, nextNumber: 7, recentIds: ['a', 'b', 'c', 'd', 'e', 'f'],
  };
  await page.addInitScript(value => { if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value); }, JSON.stringify(state));
}
const dialog = (page: Page) => page.getByRole('dialog', { name: 'All tabs overview', exact: true });
const settle = (page: Page) => page.evaluate(async () => { await Promise.all(document.getAnimations().map(animation => animation.finished.catch(() => undefined))); });
const mac = (page: Page) => page.evaluate(() => /Mac/.test(navigator.platform));

test('the grid icon opens the overview with the design bar and sections', async ({ page }) => {
  await seed(page); await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = dialog(page);
  await expect(overview).toBeVisible();
  await expect(overview.getByRole('textbox', { name: 'Filter tabs' })).toHaveAttribute('placeholder', 'Search 6 tabs');
  await expect(overview.getByRole('textbox', { name: 'Filter tabs' })).toBeFocused();
  await expect(overview.getByRole('radio', { name: 'Grid' })).toBeChecked();
  await expect(overview.getByRole('radio', { name: 'Filmstrip' })).not.toBeChecked();
  await expect(overview.getByRole('button', { name: 'Done', exact: true })).toBeVisible();
  // Sections by group: "Trailing commas 4" with Open as split, then "Other tabs 2".
  await expect(overview.locator('.overview-section-title')).toHaveText(['Trailing commas', 'Other tabs']);
  await expect(overview.locator('.overview-section-count')).toHaveText(['4', '2']);
  await expect(overview.getByRole('button', { name: 'Open as split' })).toHaveCount(1);
  await settle(page);
  // Geometry from the design: 56px bar, 320x32 search, 168px cards radius 12, 2px accent ring on the active card only.
  expect((await overview.locator('.overview-bar').boundingBox())!.height).toBe(px('overview-bar-height'));
  expect(px('overview-bar-height')).toBe(56);
  const search = (await overview.locator('.overview-search').boundingBox())!;
  expect([search.width, search.height]).toEqual([320, 32]);
  const card = overview.locator('.overview-card').first();
  expect((await card.boundingBox())!.height).toBe(168);
  await expect(card).toHaveCSS('border-top-left-radius', '12px');
  await expect(card).toHaveAttribute('data-active', 'true');
  expect(await card.evaluate(el => getComputedStyle(el).boxShadow)).toMatch(/0px 0px 0px 2px/);
  expect(await overview.locator('.overview-card[data-active="false"]').first().evaluate(el => getComputedStyle(el).boxShadow)).not.toMatch(/0px 0px 0px 2px/);
  // Card anatomy: kind line, title (13px / 500), key content (12px), padding 14 16.
  await expect(card.locator('.overview-card-head')).toContainText('Conversation');
  await expect(card.locator('.overview-card-title')).toHaveText('Config stack');
  await expect(card.locator('.overview-card-title')).toHaveCSS('font-size', '13px');
  await expect(card.locator('.overview-card-title')).toHaveCSS('font-weight', '500');
  await expect(card.locator('.preview-text')).toHaveText('Draft: I split it into two groups.');
  await expect(card.locator('.preview-card')).toHaveCount(0);
  await expect(card.locator('.overview-card-title')).toHaveCount(1);
  await expect(card.locator('.preview-text')).toHaveCSS('font-size', '12px');
  await expect(card.locator('.overview-card-face')).toHaveCSS('padding', '14px 16px');
  await expect(overview.locator('.overview-card[data-kind="task"] .overview-card-head')).toContainText('Task');
  await expectAccessible(page);
});

test('the overview shortcut opens it, Done and Escape close it, and Up leaves it shut', async ({ page }) => {
  await seed(page); await page.goto('/');
  const mac_ = await mac(page);
  await page.getByRole('tab', { name: 'Config stack' }).focus();
  await key(page, mac_ ? 'Meta+Shift+Backslash' : 'Control+Shift+A');
  await expect(dialog(page)).toBeVisible();
  await key(page, 'Escape');
  await expect(dialog(page)).not.toBeVisible();
  // ⌘↑ belongs to stepping between messages (latest Interactions page), never to the overview.
  await page.getByRole('tab', { name: 'Config stack' }).focus();
  await key(page, `${mac_ ? 'Meta' : 'Control'}+ArrowUp`);
  await expect(dialog(page)).not.toBeVisible();
  await key(page, mac_ ? 'Meta+Shift+Backslash' : 'Control+Shift+A');
  await dialog(page).getByRole('button', { name: 'Done', exact: true }).click();
  await expect(dialog(page)).not.toBeVisible();
});

test('dragging a card onto another section regroups the tab', async ({ page }) => {
  await seed(page); await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = dialog(page);
  await overview.getByRole('button', { name: 'Open Release v2.4', exact: true }).dragTo(overview.locator('.overview-section[aria-label="Trailing commas"] .overview-section-head'));
  await expect(overview.locator('.overview-section-count')).toHaveText(['5', '1']);
  await overview.getByRole('button', { name: 'Open Lexer', exact: true }).dragTo(overview.locator('.overview-section[aria-label="Other tabs"] .overview-section-head'));
  await expect(overview.locator('.overview-section-count')).toHaveText(['4', '2']);
});

test('typing filters the cards and Enter opens the first match', async ({ page }) => {
  await seed(page); await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = dialog(page);
  await overview.getByRole('textbox', { name: 'Filter tabs' }).fill('lexer');
  await expect(overview.locator('.overview-card')).toHaveCount(1);
  await expect(overview.locator('.overview-section-title')).toHaveText(['Trailing commas']);
  await overview.getByRole('textbox', { name: 'Filter tabs' }).fill('nothing matches this');
  await expect(overview.getByText('No matching tabs')).toBeVisible();
  await overview.getByRole('textbox', { name: 'Filter tabs' }).fill('release');
  await key(page, 'Enter');
  await expect(overview).not.toBeVisible();
  await expect(page.getByRole('tab', { name: 'Release v2.4' })).toHaveAttribute('aria-selected', 'true');
});

test('arrow keys move the cursor across the grid and Enter opens the card', async ({ page }) => {
  await seed(page); await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = dialog(page);
  await expect(overview.locator('.overview-card[data-cursor="true"]')).toHaveAttribute('data-card-id', 'a');
  await key(page, 'ArrowRight');
  await expect(overview.locator('.overview-card[data-cursor="true"]')).toHaveAttribute('data-card-id', 'b');
  await key(page, 'ArrowDown');
  await expect(overview.locator('.overview-card[data-cursor="true"]')).toHaveAttribute('data-card-id', /^(e|f)$/);
  await key(page, 'ArrowUp');
  await expect(overview.locator('.overview-card[data-cursor="true"]')).toHaveAttribute('data-card-id', 'b');
  await key(page, 'Enter');
  await expect(overview).not.toBeVisible();
  await expect(page.getByRole('tab', { name: 'Update fixtures' })).toHaveAttribute('aria-selected', 'true');
});

test('close shows on hover and closes the tab; Open as split merges the group', async ({ page }) => {
  await seed(page); await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = dialog(page);
  const close = overview.getByRole('button', { name: 'Close Models', exact: true });
  await expect(close).toHaveCSS('opacity', '0');
  await overview.getByRole('button', { name: 'Open Models', exact: true }).hover();
  await expect(close).toHaveCSS('opacity', '1');
  await close.click();
  await expect(overview.locator('.overview-card')).toHaveCount(5);
  await expect(overview.locator('.overview-section-count')).toHaveText(['4', '1']);
  await overview.getByRole('button', { name: 'Open as split' }).click();
  await expect(overview).not.toBeVisible();
  await expect(page.locator('.workspace-conversation[data-split]')).toHaveCount(1);
  await expect(page.locator('.workspace-pane')).toHaveCount(4);
});

test('the filmstrip lists the same order with real half-scale panes, arrows, Enter and Escape', async ({ page }) => {
  await seed(page, 'b'); await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = dialog(page);
  await overview.getByRole('radio', { name: 'Filmstrip' }).click();
  await expect(overview.getByRole('radio', { name: 'Filmstrip' })).toBeChecked();
  await expect(overview.locator('.overview-film-position')).toHaveText('Trailing commas · 2 of 4');
  await expect(overview.locator('.overview-film-hint span')).toHaveText(['← → move', '↵ open', /W close$/, 'Esc back']);
  // Same order as the grid.
  await expect(overview.locator('.overview-film-item')).toHaveCount(6);
  expect(await overview.locator('.overview-film-item').evaluateAll(els => els.map(el => (el as HTMLElement).dataset.cardId).join(''))).toBe('abcdef');
  // The centre card is full size; the others sit at .86 and .9 opacity. Viewport 520x330 over a 1040x660 pane at .5.
  const centre = overview.locator('.overview-film-item[data-cursor="true"]');
  await expect(centre).toHaveAttribute('data-card-id', 'b');
  const viewport = (await centre.locator('.overview-film-viewport').boundingBox())!;
  expect([viewport.width, viewport.height]).toEqual([520, 330]);
  await expect(centre.locator('.overview-film-pane')).toHaveCSS('transform', 'matrix(0.5, 0, 0, 0.5, 0, 0)');
  await expect(centre.locator('.overview-film-pane')).toHaveCSS('width', '1040px');
  await expect(overview.locator('.overview-film-item[data-cursor="false"]').first()).toHaveCSS('opacity', '0.9');
  expect(await overview.locator('.overview-film-item[data-cursor="false"]').first().evaluate(el => getComputedStyle(el).transform)).toBe('matrix(0.86, 0, 0, 0.86, 0, 0)');
  await expect(overview.locator('.overview-film-track')).toHaveCSS('mask-image', /linear-gradient/);
  // The pane is the real conversation pane, read-only.
  await expect(centre.locator('.overview-live[inert] textarea')).toHaveCount(1);
  await expect(centre.locator('.overview-film-label')).toHaveText('Update fixtures');
  await key(page, 'ArrowRight');
  await expect(overview.locator('.overview-film-position')).toHaveText('Trailing commas · 3 of 4');
  await key(page, 'ArrowLeft'); await key(page, 'ArrowLeft');
  await expect(overview.locator('.overview-film-position')).toHaveText('Trailing commas · 1 of 4');
  await key(page, 'Enter');
  await expect(overview).not.toBeVisible();
  await expect(page.getByRole('tab', { name: 'Config stack' })).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  await expect(overview.getByRole('radio', { name: 'Filmstrip' })).toBeChecked();
  await key(page, 'Escape');
  await expect(overview).not.toBeVisible();
});

test('Control or Command + W closes the filmstrip card in the centre', async ({ page }) => {
  await seed(page); await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = dialog(page);
  await overview.getByRole('radio', { name: 'Filmstrip' }).click();
  await key(page, `${(await mac(page)) ? 'Meta' : 'Control'}+w`);
  await expect(overview.locator('.overview-film-item')).toHaveCount(5);
  await expect(overview.locator('.overview-film-item[data-card-id="a"]')).toHaveCount(0);
  await expect(overview.locator('.overview-film-item[data-cursor="true"]')).toHaveAttribute('data-card-id', 'b');
});

for (const scheme of ['light', 'dark'] as const) {
  test(`specimen ${scheme}: grid and filmstrip are themed and accessible`, async ({ page }, info) => {
    await page.emulateMedia({ colorScheme: scheme });
    await seed(page); await page.setViewportSize({ width: 1280, height: 800 }); await page.goto('/');
    await page.getByRole('button', { name: 'All tabs', exact: true }).click();
    const overview = dialog(page);
    await expect(overview).toHaveCSS('background-color', await tokenColor(page, 'frame'));
    await expect(overview.locator('.overview-card').first()).toHaveCSS('background-color', await tokenColor(page, 'field'));
    await expect(overview.locator('.overview-card').nth(1)).toHaveCSS('background-color', await tokenColor(page, 'surface'));
    await expectAccessible(page);
    await page.screenshot({ path: info.outputPath(`overview-grid-${scheme}.png`) });
    await overview.getByRole('radio', { name: 'Filmstrip' }).click();
    await expectAccessible(page);
    await page.screenshot({ path: info.outputPath(`overview-film-${scheme}.png`) });
  });
}

test('the overview fits 320px with no page overflow', async ({ page }) => {
  await seed(page); await page.setViewportSize({ width: 320, height: 560 }); await page.goto('/');
  await page.getByRole('button', { name: 'All tabs', exact: true }).click();
  const overview = dialog(page);
  await expect(overview.getByRole('button', { name: 'Done', exact: true })).toBeInViewport();
  await expect(overview.getByRole('radio', { name: 'Filmstrip' })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await overview.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
});

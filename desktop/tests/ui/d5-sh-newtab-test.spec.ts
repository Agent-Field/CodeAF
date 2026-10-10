import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine } from './support/mock-engine';
import { designConversations, NOW, withHistory } from './support/scenarios-history';
import { installNativeWebMock } from './support/native-web-mock';

// SH-223/276 (⌘O), SH-224 (New terminal), SH-227 (closed age), SH-228 (URL row), SH-229 (From history, See all).
// Fixtures live in this spec only: the closed tab is seeded through the workspace document, History through the mock engine.
const KEY = 'codeaf.desktop.workspace.v1';
const HOUR = 3_600_000;
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });
const seedClosed = (page: Page) => page.addInitScript(([key, json]) => {
 if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, json); sessionStorage.setItem('seeded', '1'); }
}, [KEY, JSON.stringify({ tabs: [tab('a', 'Alpha')], groups: [], closed: [tab('c1', 'Fix it in the lexer', { closedAt: NOW.getTime() - HOUR })], activeId: 'a', nextNumber: 20, recentIds: ['a'] })] as const);
const field = (page: Page) => page.getByRole('combobox', { name: 'Search or start' });
const openField = (page: Page) => page.getByRole('button', { name: 'New tab', exact: true }).click();
const rowNames = (page: Page) => page.getByRole('option').allTextContents();
const primary = (page: Page) => page.evaluate(() => (/Mac/.test(navigator.platform) ? 'Meta' : 'Control')) as Promise<'Meta' | 'Control'>;
const theme = (page: Page, scheme: 'light' | 'dark') => page.emulateMedia({ colorScheme: scheme });

for (const scheme of ['light', 'dark'] as const) {
 test.describe(`new tab Start, history, closed age and URL in ${scheme}`, () => {
  test('typing offers From history rows (archived marked) and See all N in History, and the chord opens History with the words', async ({ page }) => {
   await theme(page, scheme);
   const conversations = designConversations().map(one => (one.id === 'json5' ? { ...one, archived: true } : one));
   conversations.push({ id: 'lexer2', title: 'Lexer error positions', at: designConversations()[0].at, messages: [{ role: 'user', text: 'Where does the lexer report positions?' }] });
   await installMockEngine(page, withHistory(conversations));
   await page.goto('/');
   await openField(page);
   await field(page).fill('lexer');
   const section = page.locator('.newtab-section', { hasText: 'From history' });
   await expect(section).toBeVisible();
   const rows = await rowNames(page);
   const from = rows.findIndex(name => /^Fix it in the lexer/.test(name));
   expect(from).toBeGreaterThan(0);
   const seeAll = page.getByRole('option', { name: /^See all \d+ in History/ });
   await expect(seeAll).toBeVisible();
   await expect(seeAll).toContainText(/⌘↵|Ctrl ↵/);
   expect(await page.locator('.newtab-section').allTextContents()).toContain('From history');
   await expectAccessible(page);
   await field(page).press(`${await primary(page)}+Enter`);
   await expect(page.getByRole('tab', { name: /^History/ })).toHaveAttribute('aria-selected', 'true');
   await expect(page.getByRole('searchbox').or(page.getByRole('textbox')).first()).toHaveValue('lexer');
  });

  test('a closed tab row says how long ago it closed', async ({ page }) => {
   await theme(page, scheme);
   await page.clock.setFixedTime(NOW);
   await page.route('**/api/engine/**', route => route.abort());
   await seedClosed(page);
   await page.goto('/');
   await openField(page);
   await field(page).fill('lexer');
   const row = page.getByRole('option', { name: /Fix it in the lexer/ });
   await expect(row).toContainText('closed 1h ago');
  });

  test('New terminal is a Start row while the terminal is backed and absent without it', async ({ page }) => {
   await theme(page, scheme);
   await page.route('**/api/engine/**', route => route.abort());
   await page.goto('/');
   await openField(page);
   await expect(page.getByRole('option', { name: /^New terminal/ })).toBeVisible();
   await page.getByRole('option', { name: /^New terminal/ }).click();
   await expect(page.getByRole('tab', { name: 'Terminal', exact: true })).toBeVisible();
   // Unbacking the kind in the page's own module graph is how the row's gate is exercised without a build flag.
   await page.evaluate(async () => { const path = '/src/features/tabs/kinds/terminal.ts'; (await import(path)).terminalKind.backed = false; });
   await openField(page);
   await expect(page.getByRole('option', { name: /^Open file/ })).toBeVisible();
   await expect(page.getByRole('option', { name: /^New terminal/ })).toHaveCount(0);
  });

  test('⌘O from a conversation opens a New tab asking for a file name', async ({ page }) => {
   await theme(page, scheme);
   await page.route('**/api/engine/**', route => route.abort());
   await page.goto('/');
   await expect(page.getByRole('tab')).toHaveCount(1);
   await page.keyboard.press(`${await primary(page)}+o`);
   await expect(field(page)).toBeFocused();
   await expect(page.getByText('Type part of a file name.')).toBeVisible();
   await expect(page.getByRole('tab', { name: 'New tab', exact: true })).toHaveAttribute('aria-selected', 'true');
  });

  test('a pasted https URL offers Open <host> while web is backed, and is only a question without it', async ({ page }) => {
   await theme(page, scheme);
   await page.route('**/api/engine/**', route => route.abort());
   await installNativeWebMock(page);
   await page.goto('/');
   await openField(page);
   await field(page).fill('https://pkg.go.dev/encoding/json');
   const first = (await rowNames(page))[0];
   expect(first).toMatch(/^Open pkg\.go\.dev\/encoding\/json in a web tab/);
   await expect(page.getByRole('option').first()).toHaveAttribute('aria-selected', 'true');
   await field(page).press('Enter');
   await expect(page.getByRole('tab', { name: 'pkg.go.dev', exact: true })).toBeVisible();
  });
 });
}

test('320px: history, closed and URL rows stay inside the card', async ({ page }) => {
 await page.setViewportSize({ width: 320, height: 560 });
 await installMockEngine(page, withHistory());
 await page.goto('/');
 await openField(page);
 await field(page).fill('lexer');
 await expect(page.getByRole('option', { name: /^See all \d+ in History/ })).toBeVisible();
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 const box = await page.locator('.newtab-field').boundingBox();
 expect(box!.x).toBeGreaterThanOrEqual(0);
 expect(box!.x + box!.width).toBeLessThanOrEqual(320);
});

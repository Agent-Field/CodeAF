import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';

// SH-105/106: a group label drags the whole group, and Alt+Shift+←/→ moves it one slot and announces it.
const KEY = 'codeaf.desktop.workspace.v1';
const tab = (id: string, title: string, over: Record<string, unknown> = {}) => ({ id, title, draft: '', pinned: false, kind: 'conversation', titleSource: 'manual', ...over });
const tabs = [tab('a', 'Alpha'), tab('g1', 'One', { groupId: 'G' }), tab('g2', 'Two', { groupId: 'G' }), tab('b', 'Beta'), tab('c', 'Gamma')];
async function open(page: Page, seededTabs = tabs) {
 const state = { tabs: seededTabs, groups: [{ id: 'G', title: 'Release', collapsed: false }], activeId: 'a', closed: [], nextNumber: 6, recentIds: seededTabs.map(t => t.id) };
 await page.route('**/api/engine/**', route => route.abort());
 await page.addInitScript(([key, json]) => { if (!sessionStorage.getItem('seeded')) { localStorage.setItem(key, json); sessionStorage.setItem('seeded', '1'); } }, [KEY, JSON.stringify(state)] as const);
 await page.goto('/');
}
const order = (page: Page) => page.locator('.workspace-tabstrip').evaluate(strip => Array.from(strip.querySelectorAll('[role="tab"]')).map(el => el.getAttribute('aria-label')));
const label = (page: Page) => page.locator('.workspace-group-label');

test('dragging the group label onto the right half of a tab moves the whole group, members in order', async ({ page }) => {
 await open(page);
 const source = label(page);
 const target = page.getByRole('tab', { name: 'Beta', exact: true });
 const from = (await source.boundingBox())!;
 const box = (await target.boundingBox())!;
 await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
 await page.mouse.down();
 await page.mouse.move(from.x + from.width / 2 + 8, from.y + from.height / 2 + 6, { steps: 3 });
 await page.mouse.move(box.x + box.width * 0.9, box.y + box.height / 2, { steps: 8 });
 await page.mouse.up();
 expect(await order(page)).toEqual(['Alpha', 'Beta', 'One', 'Two', 'Gamma']);
});

test('Alt+Shift+→ on the label moves the group one slot and announces it; ← moves it back', async ({ page }) => {
 await open(page);
 await label(page).focus();
 await page.keyboard.press('Alt+Shift+ArrowRight');
 expect(await order(page)).toEqual(['Alpha', 'Beta', 'One', 'Two', 'Gamma']);
 await expect(page.locator('.workspace-strip-note[role="status"]')).toHaveText('Moved group Release to position 3 of 4');
 await label(page).focus();
 await page.keyboard.press('Alt+Shift+ArrowLeft');
 expect(await order(page)).toEqual(['Alpha', 'One', 'Two', 'Beta', 'Gamma']);
});

// SH-075/076: grouping picks belong to this window; middle-click is the ordinary close.
for (const theme of ['light', 'dark'] as const) for (const mac of [false, true]) {
 test(`group selection toggles and clears in ${theme}, ${mac ? 'Command' : 'Control'}`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => Object.defineProperty(navigator, 'platform', { value }), mac ? 'MacIntel' : 'Linux x86_64');
  await open(page);
  const alpha = page.getByRole('tab', { name: 'Alpha', exact: true });
  const beta = page.getByRole('tab', { name: 'Beta', exact: true });
  const frame = beta.locator('..');
  const modifiers: ('Meta' | 'Control')[] = [mac ? 'Meta' : 'Control'];
  await beta.click({ modifiers });
  await expect(frame).toHaveAttribute('data-selected', 'true');
  await expect(beta).toHaveAttribute('aria-description', 'selected for grouping');
  await expect(alpha).toHaveAttribute('aria-selected', 'true');
  await expect.poll(() => frame.evaluate(el => {
   const probe = document.createElement('span'); probe.style.background = 'var(--field)'; el.appendChild(probe);
   const match = getComputedStyle(el).backgroundColor === getComputedStyle(probe).backgroundColor; probe.remove(); return match;
  })).toBe(true);
  await beta.click({ modifiers });
  await expect(frame).not.toHaveAttribute('data-selected');
  await alpha.click({ modifiers });
  await expect(alpha.locator('..')).toHaveAttribute('data-selected', 'true');
  await beta.click({ modifiers });
  await page.keyboard.press('Escape');
  await expect(page.locator('.workspace-tab[data-selected]')).toHaveCount(0);
  await expect(alpha).toHaveAttribute('aria-selected', 'true');
  await beta.click({ modifiers });
  await alpha.click();
  await expect(page.locator('.workspace-tab[data-selected]')).toHaveCount(0);
  await beta.click({ modifiers });
  await page.reload();
  await expect(page.locator('.workspace-tab[data-selected]')).toHaveCount(0);
 });
}

test('middle-click closes a tab, right-click leaves it, and a pin stays open', async ({ page }) => {
 await open(page);
 const beta = page.getByRole('tab', { name: 'Beta', exact: true });
 await beta.dispatchEvent('auxclick', { button: 2 });
 await expect(beta).toBeVisible();
 await beta.click({ button: 'middle' });
 await expect(beta).toHaveCount(0);
 await page.getByRole('tab', { name: 'Alpha', exact: true }).click({ button: 'right' });
 await page.getByRole('menuitem', { name: 'Pin tab', exact: true }).click();
 const alpha = page.getByRole('tab', { name: 'Alpha', exact: true });
 await alpha.click({ button: 'middle' });
 await expect(alpha).toBeVisible();
});

 test('middle-click on running work shows the closing toast and never sends Stop', async ({ page }) => {
  const engine = await installMockEngine(page, { initial: { running: true, title: '', entries: [{ Role: 'user', Text: 'Keep working' }] } });
  const running = tab('running', 'Config stack', { sessionFile: 'mock-session-1.jsonl' });
  await page.addInitScript(value => localStorage.setItem('codeaf.desktop.workspace.v1', value), JSON.stringify({ tabs: [running, tab('idle', 'Idle')], groups: [], closed: [], activeId: 'running', nextNumber: 3, recentIds: ['running', 'idle'] }));
  await page.goto('/');
  const target = page.getByRole('tab', { name: 'Config stack', exact: true });
  await target.hover();
  await expect(page.getByText('Close · keeps running', { exact: true })).toBeAttached();
  await target.click({ button: 'middle', modifiers: ['Alt'] });
  await expect(target).toHaveCount(0);
  await expect(page.getByText('closed and still running', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Stop it', exact: true })).toBeVisible();
  expect(engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/stop'))).toHaveLength(0);
 });

for (const width of [600, 1200]) {
 test(`grouped and split picks remain accessible at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 800 });
  await page.addInitScript(() => Object.defineProperty(navigator, 'platform', { value: 'Linux x86_64' }));
  const split = tab('split', 'Left and Right', { split: { layout: '1x2', focus: 0, panes: [tab('left', 'Left'), tab('right', 'Right')] } });
  await open(page, [tabs[0], tabs[1], tabs[2], split]);
  const one = page.getByRole('tab', { name: 'One', exact: true });
  await one.click({ modifiers: ['Control'] });
  await expect(one).toHaveAttribute('aria-description', 'selected for grouping');
  await expect(one.locator('..')).toHaveAttribute('data-selected', 'true');
  const segment = page.getByRole('tab', { name: width === 600 ? 'Left · Right' : 'Left', exact: true });
  await segment.click({ modifiers: ['Control'] });
  await expect(segment).toHaveAttribute('aria-description', 'selected for grouping');
  await expect(segment.locator('..')).toHaveAttribute('data-selected', 'true');
  await segment.click({ button: 'middle' });
  await expect(segment).toHaveCount(0);
 });
}

for (const theme of ['light', 'dark'] as const) {
 test(`SH-083 keyboard tab reorder keeps focus, selection and section in ${theme}`, async ({ page }) => {
  await page.emulateMedia({ colorScheme: theme });
  await open(page, [tab('p', 'Pin one', { pinned: true }), tab('q', 'Pin two', { pinned: true }), ...tabs]);
  const note = page.locator('.workspace-strip-note[role="status"]');
  await expect(note).toHaveAttribute('aria-live', 'polite');
  await expect(note).toHaveAttribute('aria-atomic', 'true');
  const move = async (name: string, key: string, message: string) => {
   const target = page.getByRole('tab', { name, exact: true });
   await target.focus();
   await page.keyboard.press(`Alt+Shift+${key}`);
   await expect(target).toBeFocused();
   await expect(note).toHaveText(message);
   await expect(page.getByRole('tab', { name: 'Alpha', exact: true })).toHaveAttribute('aria-selected', 'true');
  };
  await move('Pin one', 'ArrowRight', 'Moved to position 2 of 2');
  expect(await order(page)).toEqual(['Pin two', 'Pin one', 'Alpha', 'One', 'Two', 'Beta', 'Gamma']);
  await page.keyboard.press('Alt+Shift+ArrowRight');
  expect(await order(page)).toEqual(['Pin two', 'Pin one', 'Alpha', 'One', 'Two', 'Beta', 'Gamma']);
  await move('One', 'ArrowRight', 'Moved to position 2 of 2');
  expect(await order(page)).toEqual(['Pin two', 'Pin one', 'Alpha', 'Two', 'One', 'Beta', 'Gamma']);
  await page.keyboard.press('Alt+Shift+ArrowRight');
  expect(await order(page)).toEqual(['Pin two', 'Pin one', 'Alpha', 'Two', 'One', 'Beta', 'Gamma']);
  await move('Gamma', 'ArrowLeft', 'Moved to position 2 of 3');
  expect(await order(page)).toEqual(['Pin two', 'Pin one', 'Alpha', 'Two', 'One', 'Gamma', 'Beta']);
  await move('Gamma', 'ArrowRight', 'Moved to position 3 of 3');
  await move('Gamma', 'ArrowLeft', 'Moved to position 2 of 3');
 });
}

for (const width of [600, 1200]) {
 test(`SH-083 a split reorders as one tab and preserves its focused segment at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 800 });
  await page.addInitScript(() => Object.defineProperty(navigator, 'platform', { value: 'Linux x86_64' }));
  const split = tab('split', 'Left and Right', { split: { layout: '1x2', focus: 0, panes: [tab('left', 'Left'), tab('right', 'Right')] } });
  await open(page, [tabs[0], split, tab('b', 'Beta')]);
  const name = width === 600 ? 'Left · Right' : 'Right';
  const target = page.getByRole('tab', { name, exact: true });
  await target.click({ modifiers: ['Control'] });
  await target.focus();
  await page.keyboard.press('Alt+Shift+ArrowRight');
  await expect(target).toBeFocused();
  await expect(target).toHaveAttribute('aria-description', 'selected for grouping');
  await expect(page.locator('.workspace-strip-note[role="status"]')).toHaveText('Moved to position 3 of 3');
  expect(await order(page)).toEqual(width === 600 ? ['Alpha', 'Beta', 'Left · Right'] : ['Alpha', 'Beta', 'Left', 'Right']);
 });
}

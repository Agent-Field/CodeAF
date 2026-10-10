import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openApp } from './support/conversation';
import { plainReply } from './support/scenarios';

// Iteration 2 I2.6: ⌘J walks Next up, ⌘[ / ⌘] step focus history (Alt arrows off a Mac), ⌘↑ is "up a level" on Home only.
const mac = process.platform === 'darwin';
const primary = mac ? 'Meta' : 'Control';
const backChord = mac ? `${primary}+BracketLeft` : 'Alt+ArrowLeft';
const forwardChord = mac ? `${primary}+BracketRight` : 'Alt+ArrowRight';
const tabs = (page: Page) => page.getByRole('tab');
const selectedIndex = (page: Page) => tabs(page).evaluateAll(all => all.findIndex(tab => tab.getAttribute('aria-selected') === 'true'));

test.beforeEach(async ({ page }) => { await installMockEngine(page, plainReply()); });

test('in a chat, back and forward chords step between the tabs the person visited', async ({ page }) => {
  await openApp(page);
  const first = await selectedIndex(page);
  await page.keyboard.press(`${primary}+KeyT`);
  await expect.poll(() => tabs(page).count()).toBeGreaterThan(1);
  const second = await selectedIndex(page);
  expect(second).not.toBe(first);
  await page.keyboard.press(backChord);
  await expect.poll(() => selectedIndex(page)).toBe(first);
  await page.keyboard.press(forwardChord);
  await expect.poll(() => selectedIndex(page)).toBe(second);
});

test('in a chat, ⌘↑ stays message navigation and ⌘J with nothing waiting changes nothing', async ({ page }) => {
  await openApp(page);
  const before = await tabs(page).count();
  await page.keyboard.press(`${primary}+ArrowUp`);
  await page.keyboard.press(`${primary}+KeyJ`);
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeVisible();
  expect(await tabs(page).count()).toBe(before);
});

test('the retired ⌘I chord does nothing', async ({ page }) => {
  await openApp(page);
  const before = await tabs(page).count();
  await page.keyboard.press(`${primary}+KeyI`);
  expect(await tabs(page).count()).toBe(before);
});

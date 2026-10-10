import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import { installMockEngine, GLM, GLM_FLASH, MODEL } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openApp, send } from './support/conversation';
import { expectAccessible, expectNoUnstyledControls } from './contracts';

const OTHER = 'moonshotai/kimi-k3';
const scenario = () => ({
  ...plainReply(),
  initial: { entries: [], title: '' },
  models: [
    { id: MODEL, name: 'DeepSeek V4.1 Flash' },
    { id: GLM_FLASH, name: 'GLM 5.3 Flash' },
    { id: GLM, name: 'GLM 5.3' },
    { id: OTHER, name: 'Kimi K3', efforts: ['low', 'medium', 'high'] },
  ],
});
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
const chip = (page: Page, label: string) => page.getByRole('button', { name: `Model: ${label}` });
const puts = (calls: { method: string; path: string; body: Record<string, unknown> }[]) => calls.filter(call => call.method === 'PUT');

async function openSettings(page: Page) {
  await openPage(page, 'Settings');
  await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
}

test('the settings page lists the pinned models and one row per role', async ({ page }) => {
  await installMockEngine(page, scenario());
  await openApp(page);
  await openSettings(page);
  const pinned = page.getByRole('region', { name: 'Pinned' });
  await expect(pinned.getByRole('button', { name: /^Pinned model/ })).toHaveText(['GLM 5.3 Flash', 'DeepSeek V4.1 Flash', 'GLM 5.3']);
  // Each job sits under its section, in the engine's order.
  await expect(page.getByRole('heading', { level: 2 })).toHaveText(['Appearance', 'Engine', 'Pinned', 'Conversation and tasks', 'Naming and summaries', 'Places organization', 'Memory, routing and safety']);
  for (const [section, name] of [['Conversation and tasks', 'Tasks'], ['Naming and summaries', 'Chat titles'], ['Places organization', 'Chat filing'], ['Memory, routing and safety', 'Memory']]) {
    await expect(page.getByRole('region', { name: section }).getByText(name, { exact: true })).toBeVisible();
  }
  await expect(page.getByRole('button', { name: 'Model for Tasks' })).toHaveText('DeepSeek V4.1 Flash');
  // Nothing differs from the default yet, so there is no Reset and no receipt.
  await expect(page.getByRole('button', { name: /^Reset/ })).toHaveCount(0);
  await expect(page.locator('.settings-receipt')).toHaveText('');
});

test('changing a role saves at once, shows the receipt, offers effort, and can be reset', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openApp(page);
  await openSettings(page);
  await page.getByRole('button', { name: 'Model for Tasks' }).click();
  await page.getByRole('combobox', { name: 'Search models' }).fill('kimi');
  const rows = page.getByRole('listbox', { name: 'Models' }).getByRole('option');
  await expect(rows).toHaveText(['Kimi K3']);
  await rows.first().click();
  await expect(page.locator('.settings-receipt')).toHaveText('Saved · applies to the next call');
  expect(puts(engine.calls).at(-1)).toMatchObject({ path: expect.stringMatching(/\/models\/roles\/tasks$/), body: { model: OTHER } });
  await expect(page.getByRole('button', { name: 'Model for Tasks' })).toHaveText('Kimi K3');
  const effort = page.getByRole('radiogroup', { name: 'Effort for Tasks' });
  await effort.getByRole('radio', { name: 'High' }).click();
  await expect.poll(() => puts(engine.calls).at(-1)?.body).toEqual({ model: OTHER, effort: 'high' });
  await expect(effort.getByRole('radio', { name: 'High' })).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('button', { name: 'Reset Tasks' }).click();
  await expect.poll(() => puts(engine.calls).at(-1)?.body).toMatchObject({ model: '' });
  await expect(page.getByRole('button', { name: 'Model for Tasks' })).toHaveText('DeepSeek V4.1 Flash');
  await expect(page.getByRole('button', { name: /^Reset/ })).toHaveCount(0);
});

test('a pinned slot takes a catalog model and the composer picker follows in order', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openApp(page);
  await openSettings(page);
  await page.getByRole('button', { name: 'Pinned model 3' }).click();
  await page.getByRole('listbox', { name: 'Models' }).getByRole('option', { name: 'Kimi K3' }).click();
  await expect.poll(() => puts(engine.calls).at(-1)?.body).toEqual({ models: [GLM_FLASH, MODEL, OTHER] });
  await expect(page.getByRole('button', { name: 'Reset pinned models' })).toBeVisible();
  await page.getByRole('button', { name: 'Now', exact: true }).click();
  await send(page, 'hello');
  await chip(page, 'DeepSeek v4.1 Flash').click();
  await expect(page.getByRole('radiogroup', { name: 'Pinned models', exact: true }).getByRole('radio')).toHaveText(['GLM Flash', 'DS Flash', 'kimi-k3']);
});

test('the picker shows the three pinned labels in order and the chords pick them', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openApp(page);
  await send(page, 'hello');
  await expect.poll(() => engine.calls.some(call => call.path.endsWith('/models/pinned'))).toBe(true);
  await expect(chip(page, 'DeepSeek v4.1 Flash')).toHaveText('DS Flash');
  await chip(page, 'DeepSeek v4.1 Flash').click();
  const popover = page.getByRole('dialog', { name: 'Model' });
  await expect(popover.getByRole('radiogroup', { name: 'Pinned models', exact: true }).getByRole('radio')).toHaveText(['GLM Flash', 'DS Flash', 'GLM 5.3']);
  await expect(popover.getByText('All models…')).toBeVisible();
  await page.keyboard.press('Escape');
  await page.keyboard.press(`Alt+${mod}+3`);
  await expect(chip(page, 'GLM 5.3')).toHaveText('GLM 5.3');
  await expect.poll(() => puts(engine.calls).length).toBe(1);
  await page.keyboard.press(`Alt+${mod}+2`);
  await expect(chip(page, 'DeepSeek v4.1 Flash')).toHaveText('DS Flash');
  await expect.poll(() => puts(engine.calls).map(call => call.body.model)).toEqual([GLM, MODEL]);
});

test('settings is one centred column, accessible in light and dark, with no horizontal overflow at 320px', async ({ page }) => {
  await installMockEngine(page, scenario());
  await page.emulateMedia({ colorScheme: 'light' });
  await openApp(page);
  await openSettings(page);
  await expect(page.getByRole('button', { name: 'Model for Tasks' })).toBeVisible();
  await expectAccessible(page);
  await expectNoUnstyledControls(page);
  await page.getByRole('button', { name: 'Model for Tasks' }).click();
  await expectAccessible(page);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'Model for Tasks' })).toBeFocused();
  await page.emulateMedia({ colorScheme: 'dark' });
  await expectAccessible(page);
  const column = await page.locator('.settings-page').evaluate(node => node.getBoundingClientRect().width);
  expect(column).toBeLessThanOrEqual(640);
  await page.setViewportSize({ width: 320, height: 700 });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
});

test('a job nothing calls yet says so, and a split job follows the one it came from until chosen', async ({ page }) => {
  await installMockEngine(page, scenario());
  await openApp(page);
  await openSettings(page);
  const places = page.getByRole('region', { name: 'Places organization' });
  await expect(places.locator('[data-role="placefiling"]')).toContainText('Not in use yet. Your choice is kept for when it is.');
  await expect(page.locator('[data-role="tasks"]')).not.toContainText('Not in use yet');
  await page.getByRole('button', { name: 'Model for Chat titles' }).click();
  await page.getByRole('listbox', { name: 'Models' }).getByRole('option', { name: 'Kimi K3' }).click();
  await expect(page.locator('.settings-receipt')).toHaveText('Saved · applies to the next call');
  // Summaries was split from the titles row; it runs on the titles choice until it has its own.
  await page.reload();
  await openSettings(page);
  await expect(page.getByRole('button', { name: 'Model for Summaries' })).toHaveText('Kimi K3');
  await expect(page.locator('[data-role="summaries"]')).not.toContainText('Not in use yet');
});

test('the Places organization choices save at once, refuse what they cannot hold, and reset', async ({ page }) => {
  const engine = await installMockEngine(page, scenario());
  await openApp(page);
  await openSettings(page);
  const places = page.getByRole('region', { name: 'Places organization' });
  const auto = places.getByRole('radiogroup', { name: 'File chats without asking' });
  // Nothing reorganises by itself: automatic filing starts off and is labelled as a provisional choice.
  await expect(auto.getByRole('radio', { name: 'Off' })).toHaveAttribute('aria-checked', 'true');
  await expect(places.locator('[data-setting="autoFile"]')).toContainText('Provisional default: Off');
  await expect(places.locator('[data-setting="clusterOffers"]')).not.toContainText('Provisional default');
  await auto.getByRole('radio', { name: 'On' }).click();
  await expect.poll(() => puts(engine.calls).at(-1)).toMatchObject({ path: expect.stringMatching(/\/places\/policy\/autoFile$/), body: { value: true } });
  await expect(page.locator('.settings-receipt')).toHaveText('Saved · applies to the next call');

  const cap = places.getByRole('spinbutton', { name: 'Top-level places codeaf may create' });
  await cap.fill('900');
  await cap.press('Enter');
  await expect(page.locator('.settings-receipt')).toHaveText('Not saved · maxAiTopLevel is out of range');
  // A refused number is put back, so the field never shows what was not saved.
  await expect(cap).toHaveValue('6');
  await cap.fill('4');
  await cap.press('Enter');
  await expect.poll(() => puts(engine.calls).at(-1)?.body).toEqual({ value: 4 });
  await page.getByRole('button', { name: 'Reset Top-level places codeaf may create' }).click();
  await expect.poll(() => puts(engine.calls).at(-1)?.body).toEqual({ value: null });
  await expect(cap).toHaveValue('6');
});

test('an engine without the Places organization routes still shows every model choice', async ({ page }) => {
  await installMockEngine(page, { ...scenario(), placesPolicy: false });
  await openApp(page);
  await openSettings(page);
  const places = page.getByRole('region', { name: 'Places organization' });
  await expect(places.getByText('How codeaf offers places cannot be changed from this engine yet.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Model for Chat filing' })).toBeVisible();
});

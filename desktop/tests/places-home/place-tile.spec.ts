import { test, expect, type Page } from '@playwright/test';
import { tokenColor } from '../ui/contracts';

// Home draws the same 84px tile the Components specimen measures (Places 8a, P-CMP-7..9).
async function open(page: Page, scenario: string, theme: 'light' | 'dark' = 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto(`/?scenario=${scenario}`);
  await page.getByTestId('home-specimen').waitFor();
}
const tile = (page: Page, id: string) => page.locator(`[data-place-id="${id}"]`);
const log = (page: Page) => page.getByRole('list', { name: 'Callback log' }).locator('li');

async function shadow(page: Page, value: string) {
  return page.evaluate(declared => {
    const probe = document.createElement('span');
    probe.style.boxShadow = declared;
    document.body.append(probe);
    const painted = getComputedStyle(probe).boxShadow;
    probe.remove();
    return painted;
  }, value);
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: a place home tile rests, hovers, focuses, selects and takes a drop`, async ({ page }) => {
    await open(page, 'place', theme);
    const software = tile(page, 'pl_software');
    await expect(software).toHaveCSS('height', '84px');
    await expect(software).toHaveCSS('border-top-left-radius', '12px');
    await expect(software).toHaveCSS('background-color', await tokenColor(page, 'surface'));
    await expect(software.locator('.places-tile-name')).toHaveText('Software');
    await expect(software.locator('.places-tile-name')).toHaveCSS('color', await tokenColor(page, 'ink'));
    await expect(software.locator('.places-tile-meta')).toHaveText('28 chats');
    await expect(software.locator('.place-swatch')).toHaveCSS('width', '12px');
    await expect(software.locator('.status-mark')).toHaveCSS('width', '6px');
    await expect(software.locator('.status-mark')).toHaveCSS('height', '6px');
    await expect(software.locator('.status-mark')).toHaveCSS('color', await tokenColor(page, 'amber'));
    await expect(tile(page, 'pl_marketing').locator('.status-mark')).toHaveCount(0);
    await expect(tile(page, 'pl_marketing').locator('.places-tile-meta')).toHaveText('14 chats');
    await expect(tile(page, 'pl_release').locator('.places-tile-meta')).toHaveText('6 chats · also in Software');

    const add = page.locator('.home-places .places-tile[data-mode="new"]');
    await expect(add).toHaveCSS('height', '84px');
    await expect(add).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await expect(add.locator('.places-tile-new-label')).toHaveText('New place');
    await expect(add.locator('.app-icon')).toHaveCSS('width', '16px');

    await software.hover();
    await expect(software).toHaveCSS('background-color', await tokenColor(page, 'field'));
    const main = software.locator('.places-tile-main');
    await main.focus();
    await expect(main).toHaveCSS('box-shadow', await shadow(page, '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 calc(var(--focus-ring-width) + var(--focus-halo-width)) var(--accent-soft)'));
    await main.press('Space');
    await expect(software).toHaveAttribute('data-selected', 'true');
    await expect(software).toHaveCSS('box-shadow', await shadow(page, 'var(--sh-1), 0 0 0 var(--places-tile-ring-selected) var(--accent)'));
    await page.keyboard.press('Escape');

    const target = tile(page, 'pl_marketing');
    const data = await page.evaluateHandle(() => new DataTransfer());
    await page.locator('[data-chat-id="chat_commas"]').dispatchEvent('dragstart', { dataTransfer: data });
    await target.dispatchEvent('dragenter', { dataTransfer: data });
    await expect(target).toHaveAttribute('data-drop', 'true');
    await expect(target.locator('.places-tile-drop')).toHaveText('Add here');
    await target.dispatchEvent('drop', { dataTransfer: data });
    await expect(log(page).last()).toHaveText('file:chat:chat_commas:pl_marketing:add');
  });

  test(`${theme}: All places tiles keep the rolled-up count and the failed-child meta`, async ({ page }) => {
    await open(page, 'root', theme);
    const codeaf = tile(page, 'pl_codeaf');
    await expect(codeaf).toHaveCSS('height', '84px');
    await expect(codeaf.locator('.places-tile-meta')).toHaveText('4 places · 61 chats');
    await expect(codeaf.locator('.status-mark')).toHaveCSS('color', await tokenColor(page, 'amber'));
    await expect(tile(page, 'pl_reports').locator('.places-tile-meta')).toHaveText('47 places · 212 chats');
    await expect(tile(page, 'pl_reports').locator('.status-mark')).toHaveCount(0);
    await expect(page.locator('.home-places .places-tile[data-mode="new"]')).toHaveCSS('height', '84px');
  });
}

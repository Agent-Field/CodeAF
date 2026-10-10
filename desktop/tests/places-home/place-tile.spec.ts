import { test, expect, type Locator, type Page } from '@playwright/test';
import { tokenColor } from '../ui/contracts';

// Home draws the same 84px tile the Components specimen measures (Places 8a, P-CMP-7..9).
async function open(page: Page, scenario: string, theme: 'light' | 'dark' = 'light') {
  await page.emulateMedia({ colorScheme: theme });
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.goto(`/?scenario=${scenario}`);
  await page.getByTestId('home-specimen').waitFor();
}
const tile = (page: Page, id: string) => page.locator(`[data-place-id="${id}"]`);

/** A point on the row the cursor can actually press. The Home dock covers the middle of a chat, and a row below the fold is not under the cursor until it is scrolled into view. */
async function pointOn(locator: Locator) {
  const find = () => locator.evaluate(el => {
    const r = el.getBoundingClientRect();
    for (let y = r.top + 2; y < r.bottom - 1; y += 4) {
      for (const x of [r.left + 16, r.left + r.width / 2]) {
        const top = document.elementsFromPoint(x, y)[0];
        if (top && (top === el || el.contains(top))) return { x, y };
      }
    }
    return null;
  });
  let point = await find();
  if (!point) {
    await locator.evaluate(el => el.scrollIntoView({ block: 'nearest', inline: 'nearest' }));
    point = await find();
  }
  if (!point) throw new Error('no point on the dragged row is under the cursor');
  return point;
}
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
    await expect(main).toHaveCSS('box-shadow', await shadow(page, '0 0 0 var(--focus-ring-width) var(--accent), 0 0 0 var(--focus-halo-width) var(--accent-soft)'));
    await main.press('Space');
    await expect(software).toHaveAttribute('data-selected', 'true');
    await expect(software).toHaveCSS('box-shadow', await shadow(page, 'var(--sh-1), 0 0 0 var(--places-tile-ring-selected) var(--accent)'));
    await page.keyboard.press('Escape');

    const target = tile(page, 'pl_marketing');
    const source = page.locator('[data-chat-id="chat_commas"]');
    const start = await pointOn(source);
    const end = await pointOn(target);
    await page.mouse.move(start.x, start.y);
    await page.mouse.down();
    await page.mouse.move(start.x + 8, start.y + 4, { steps: 3 });
    await page.mouse.move(end.x, end.y, { steps: 8 });
    await expect(target).toHaveAttribute('data-drop', 'true');
    await expect(target.locator('.places-tile-drop')).toHaveText('Add here');
    await page.mouse.up();
    await expect(log(page).last()).toHaveText('file:chat:chat_commas:pl_marketing:add');
  });

  test(`${theme}: All places tiles keep the rolled-up count and the failed-child meta`, async ({ page }) => {
    // The All places root draws the taller root tile (I2, root-home-tile-height); a place's own Home keeps the 84px tile.
    await open(page, 'root', theme);
    const codeaf = tile(page, 'pl_codeaf');
    await expect(codeaf).toHaveCSS('height', '108px');
    await expect(codeaf.locator('.places-tile-meta')).toHaveText('4 places · 61 chats');
    await expect(codeaf.locator('.status-mark')).toHaveCSS('color', await tokenColor(page, 'amber'));
    await expect(tile(page, 'pl_reports').locator('.places-tile-meta')).toHaveText('47 places · 212 chats');
    await expect(tile(page, 'pl_reports').locator('.status-mark')).toHaveCount(0);
    await expect(page.locator('.home-places .places-tile[data-mode="new"]')).toHaveCSS('height', '84px');
  });
}

import { test, expect, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { richReply } from './support/scenarios-v2';
import { openApp, send } from './support/conversation';

const path = 'internal/auth/auth_test.go';
async function openFile(page: Page, editors: object[], local = true) {
  await installMockEngine(page, richReply());
  await page.route('**/api/engine/sessions/*/editors?**', route => route.fulfill({
    json: { editors, local, open: local },
  }));
  await openApp(page);
  await send(page, 'Fix the flaky login test and draw a logo');
  await page.getByRole('button', { name: /^Changed 1 file/ }).click();
  await page.locator('.changes').getByRole('button', { name: /^auth_test\.go/ }).click({ modifiers: ['ControlOrMeta'] });
}

for (const theme of ['light', 'dark'] as const) test.describe(theme, () => {
  test.beforeEach(async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  });

  test(`Open in lists the engine's editors default first, then Copy path · ${theme}`, async ({ page }) => {
    await openFile(page, [{ id: 'preview', name: 'Preview' }, { id: 'photoshop', name: 'Photoshop', default: true }]);
    const opened: unknown[] = [];
    await page.route('**/api/engine/sessions/*/editors/open', async route => {
      opened.push(route.request().postDataJSON());
      await route.fulfill({ json: { accepted: true } });
    });
    const trigger = page.locator('.file-head').getByRole('button', { name: 'Open in', exact: true });
    await trigger.focus();
    await page.keyboard.press('Enter');
    const menu = page.getByRole('menu', { name: 'Open in', exact: true });
    const items = menu.getByRole('menuitem');
    await expect(items).toHaveText(['Photoshopdefault', 'Preview', /Copy path/, 'Copy relative path']);
    await expect(items.nth(0)).toBeFocused();
    await expect(menu.getByRole('separator')).toHaveCount(1);
    await menu.evaluate(async el => { await Promise.all(el.getAnimations({ subtree: true }).map(animation => animation.finished.catch(() => undefined))); });
    const geometry = await menu.evaluate(el => {
      const row = el.querySelector('.menu-item')!;
      const tag = el.querySelector('.menu-detail')!;
      const probe = document.createElement('span');
      probe.style.backgroundColor = 'var(--field-2)';
      probe.style.color = 'var(--ink-3)';
      el.append(probe);
      const field = getComputedStyle(probe).backgroundColor;
      const muted = getComputedStyle(probe).color;
      probe.remove();
      return { width: el.getBoundingClientRect().width, row: row.getBoundingClientRect().width,
        height: row.getBoundingClientRect().height, fill: getComputedStyle(row).backgroundColor,
        field: field, tag: getComputedStyle(tag).color,
        muted: muted };
    });
    expect(geometry.width).toBe(230);
    expect(geometry.row).toBe(220);
    expect(geometry.height).toBe(28);
    expect(geometry.fill).toBe(geometry.field);
    expect(geometry.tag).toBe(geometry.muted);
    await page.keyboard.press('ArrowDown');
    await expect(items.nth(1)).toBeFocused();
    await page.keyboard.press('Enter');
    await expect.poll(() => opened).toEqual([{ path, id: 'preview' }]);
    await expect(trigger).toBeFocused();
    await page.keyboard.press('Enter');
    await page.keyboard.press('Escape');
    await expect(menu).toBeHidden();
    await expect(trigger).toBeFocused();
  });

  test(`a remote engine's Open in holds only the copy items · ${theme}`, async ({ page }) => {
    // Even an inconsistent remote response must never offer an editor launch.
    await openFile(page, [{ id: 'preview', name: 'Preview', default: true }], false);
    await page.locator('.file-head').getByRole('button', { name: 'Open in', exact: true }).click();
    const menu = page.getByRole('menu', { name: 'Open in', exact: true });
    await expect(menu.getByRole('menuitem')).toHaveText([/Copy path/, 'Copy relative path']);
    await expect(menu.getByRole('separator')).toHaveCount(0);
  });

  test(`one local editor keeps Open in editor · ${theme}`, async ({ page }) => {
    await openFile(page, [{ id: 'preview', name: 'Preview', default: true }]);
    const opened: unknown[] = [];
    await page.route('**/api/engine/sessions/*/editors/open', async route => {
      opened.push(route.request().postDataJSON());
      await route.fulfill({ json: { accepted: true } });
    });
    await page.locator('.file-head').getByRole('button', { name: 'Open in editor', exact: true }).click();
    await expect.poll(() => opened).toEqual([{ path, id: 'preview' }]);
    await expect(page.getByRole('menu')).toHaveCount(0);
  });
});

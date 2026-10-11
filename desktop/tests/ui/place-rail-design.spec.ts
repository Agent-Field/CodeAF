import { test, expect, type Locator, type Page } from '@playwright/test';

async function mount(page: Page, theme = 'light', empty = false) {
  await page.route('**/api/engine/**', route => route.abort());
  await page.goto('/');
  await page.evaluate(async ({ theme, empty }) => {
    const path = '/tests/ui/fixtures/place-rail-design-mount.tsx';
    const { mountPlaceRail } = await import(path);
    mountPlaceRail(theme, empty);
  }, { theme, empty });
  await expect(page.getByRole('button', { name: 'Switch place' })).toBeVisible();
}
const row = (page: Page, name: string) => page.locator('.rail .rail-row-main').filter({ has: page.locator('.rail-place-name', { hasText: new RegExp(`^${name}`) }) });
const calls = (page: Page) => page.locator('#rail-calls');
async function beginDrag(page: Page, source: Locator) {
  const box = (await source.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 8, box.y + box.height / 2);
  await expect(page.locator('.pointer-drag-ghost')).toBeVisible();
}
async function land(page: Page, target: string) {
  const box = (await page.locator(`.rail ${target}`).first().boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 5 });
  await page.mouse.up();
  await expect(page.locator('.pointer-drag-ghost')).toHaveCount(0);
}
async function drag(page: Page, source: Locator, target: string) {
  await beginDrag(page, source);
  await land(page, target);
}

for (const theme of ['light', 'dark']) {
  test(`switcher preserves section anatomy, identity, status and keyboard return · ${theme}`, async ({ page }) => {
    await mount(page, theme);
    await page.getByRole('button', { name: 'Switch place' }).click();
    const menu = page.getByRole('menu', { name: 'Place switcher' });
    await expect(menu.getByText('Pinned', { exact: true })).toBeVisible();
    await expect(menu.getByText('Open', { exact: true })).toBeVisible();
    // Measure the settled surface, rather than the shared popup's opening transform.
    await menu.evaluate(el => Promise.all(el.getAnimations().map(animation => animation.finished)));
    expect((await menu.getByText('Pinned', { exact: true }).boundingBox())!.height).toBe(21);
    const current = menu.getByRole('menuitemcheckbox', { name: /^codeaf/ });
    await expect(current).toHaveAttribute('aria-checked', 'true');
    expect(await current.evaluate(el => { const s = getComputedStyle(el), r = el.getBoundingClientRect(); return [r.width, r.height, s.fontSize, s.gap, s.borderRadius]; })).toEqual([280, 32, '13px', '10px', '7px']);
    expect(await menu.evaluate(el => { const s = getComputedStyle(el), r = el.getBoundingClientRect(); return [r.width, s.borderRadius, s.padding]; })).toEqual([290, '12px', '5px']);
    expect(await menu.locator('.keyboard-shortcut').first().evaluate(el => getComputedStyle(el).fontSize)).toBe('11px');
    const config = menu.getByRole('menuitemcheckbox', { name: /Config parser/ });
    await expect(config.locator('.place-swatch')).toHaveAttribute('data-tint-name', 'tide');
    await expect(config.locator('.status-mark')).toHaveAccessibleName('2 need you in Config parser');
    expect((await config.locator('.status-mark').boundingBox())!.width).toBe(6);
    await expect(menu.getByRole('menuitemcheckbox', { name: /Marketing/ }).locator('.status-mark')).toHaveAccessibleName('1 failed task in Marketing');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', { name: 'Switch place' })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(menu.getByRole('menuitemcheckbox', { name: /^Now/ })).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(current).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(calls(page)).toHaveText('go:codeaf');
  });

  test(`rail roves without navigating, Space looks, Alt arrows reorder · ${theme}`, async ({ page }) => {
    await mount(page, theme);
    const current = row(page, 'codeaf');
    await current.focus();
    await page.keyboard.press('ArrowDown');
    await expect(row(page, 'Personal')).toBeFocused();
    await expect(calls(page)).toBeEmpty();
    await page.keyboard.press('Alt+ArrowUp');
    await expect(calls(page)).toHaveText('pin:personal:0');
    await expect(row(page, 'Personal')).toBeFocused();
    await page.keyboard.press('End');
    await expect(page.getByRole('button', { name: /^All places/ })).toBeFocused();
    await page.keyboard.press('Home');
    await expect(row(page, 'Now')).toBeFocused();
    await row(page, 'Config parser').focus();
    await page.keyboard.press('Space');
    await expect(calls(page)).toHaveText('pin:personal:0|look:config');
    await page.keyboard.press('Enter');
    await expect(calls(page)).toHaveText('pin:personal:0|look:config|go:config');
    await expect(page.locator('.rail .nav-item[tabindex="0"]')).toHaveCount(1);
  });

  test(`rail drop inserts once, unpins once, and files once · ${theme}`, async ({ page }) => {
    await mount(page, theme);
    await drag(page, row(page, 'Config parser'), '[aria-label="Pinned"] .rail-row');
    await expect(calls(page)).toHaveText('pin:config:0');
    await expect(page.locator('[aria-label="Pinned"] .rail-place-name').first()).toHaveText('Config parser · codeaf');
    await drag(page, row(page, 'Config parser'), '[aria-label="Open"] .rail-row');
    await expect(calls(page)).toHaveText('pin:config:0|unpin:config');
    await drag(page, page.getByRole('button', { name: 'Drag chat', exact: true }), '[aria-label="Open"] .rail-row');
    await expect(calls(page)).toHaveText('pin:config:0|unpin:config|file:chat1:config');
  });
}

test('a drag creates the first pin slot without a permanent empty heading', async ({ page }) => {
  await mount(page, 'light', true);
  await expect(page.getByText('Pinned', { exact: true })).toHaveCount(0);
  await beginDrag(page, row(page, 'Config parser'));
  await expect(page.getByText('Pinned', { exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.mouse.up();
  await expect(page.getByText('Pinned', { exact: true })).toHaveCount(0);
  await expect(calls(page)).toBeEmpty();
  await beginDrag(page, row(page, 'Config parser'));
  await expect(page.getByText('Pinned', { exact: true })).toBeVisible();
  await land(page, '[aria-label="Pinned"] .rail-rows');
  await expect(calls(page)).toHaveText('pin:config:0');
});

test('keyboard row menu uses the shared tint squares and routes close independently', async ({ page }) => {
  await mount(page);
  await row(page, 'Config parser').focus();
  await page.keyboard.press('Shift+F10');
  const menu = page.getByRole('menu', { name: 'Config parser actions' });
  await expect(menu.getByRole('menuitem', { name: 'Quick Look Space' })).toBeVisible();
  await expect(menu.getByRole('radio')).toHaveCount(5);
  await expect(menu.getByRole('radio', { name: 'Graphite' })).toHaveCount(0);
  await menu.getByRole('menuitem', { name: 'Close', exact: true }).click();
  await expect(calls(page)).toHaveText('close:config');
});

test('a pinned-only rail exposes Open during drag so the last place can be unpinned', async ({ page }) => {
  await mount(page);
  await page.getByRole('button', { name: 'Close all', exact: true }).click();
  await expect(page.getByText('Open', { exact: true })).toHaveCount(0);
  await beginDrag(page, row(page, 'codeaf'));
  await expect(page.getByText('Open', { exact: true })).toBeVisible();
  await land(page, '[aria-label="Open"] .rail-rows');
  await expect(calls(page)).toHaveText('close:all|unpin:codeaf');
});

test('unsupported pointer payloads are ignored instead of filing or pinning', async ({ page }) => {
  await mount(page);
  await drag(page, page.getByRole('button', { name: 'Drag unsupported item', exact: true }), '[aria-label="Pinned"] .rail-row');
  await expect(calls(page)).toBeEmpty();
});

for (const theme of ['light', 'dark']) {
  test(`a touch hold opens the extracted place row menu without navigating · ${theme}`, async ({ browser }) => {
    const context = await browser.newContext({ hasTouch: true, viewport: { width: 1200, height: 800 } });
    try {
      const page = await context.newPage();
      await mount(page, theme);
      const config = row(page, 'Config parser');
      expect((await config.boundingBox())!.height).toBe(40);
      await config.dispatchEvent('pointerdown', { pointerType: 'touch', button: 0, pointerId: 1 });
      const menu = page.getByRole('menu', { name: 'Config parser actions' });
      await expect(menu).toBeVisible();
      await config.dispatchEvent('pointerup', { pointerType: 'touch', button: 0, pointerId: 1 });
      // The release click belongs to the hold, so it must not also navigate to the place.
      await config.dispatchEvent('click', { button: 0 });
      await expect(calls(page)).toBeEmpty();
      await menu.getByRole('menuitem', { name: 'Close', exact: true }).click();
      await expect(calls(page)).toHaveText('close:config');
      await expect(config).toHaveCount(0);
    } finally {
      await context.close();
    }
  });
}

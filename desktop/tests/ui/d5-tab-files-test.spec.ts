import { test, expect, type Page } from '@playwright/test';
import { changedPath as path, startFileFollowups, openFollowupFile, expectFileAccessible } from './support/file-followups';
import { expectNoHorizontalOverflow } from './support/conversation';

async function openFile(page: Page, editors: object[], local = true) {
  const fixture = await startFileFollowups(page, editors, local);
  await page.getByRole('button', { name: /^Changed 1 file/ }).click();
  await page.locator('.changes').getByRole('button', { name: /^auth_test\.go/ }).click({ modifiers: ['ControlOrMeta'] });
  await expect(page.locator('.file-diff-row[data-kind="add"] .file-text')).toContainText('fixedClock()');
  return fixture;
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
    await expectFileAccessible(page);
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

  test("the can't-be-shown line reads Too large to show · 3.4 MB", async ({ page }) => {
    await startFileFollowups(page);
    await openFollowupFile(page, 'art/logo.psd');
    await expect(page.locator('.file-message')).toHaveText('Too large to show · 3.4 MB');
    await expect(page.locator('.file-head').getByRole('button', { name: 'Open in', exact: true })).toBeVisible();
    await expectFileAccessible(page);
  });

  test('outside git reads <dir> · not in git with no toggle', async ({ page }) => {
    await startFileFollowups(page);
    await openFollowupFile(page, 'scratch/notes.go');
    await expect(page.locator('.file-dir')).toHaveText('scratch · not in git');
    await expect(page.locator('.file-head').getByRole('radiogroup')).toHaveCount(0);
    await expect(page.locator('.file-count')).toHaveCount(0);
    await expect(page.locator('.file-plain-row')).toContainText(['package notes']);
    await expectFileAccessible(page);
  });

  test('json/text/image files get their icons on tab and header', async ({ page }) => {
    await startFileFollowups(page);
    // Q34 and TF10-1 name the plain file glyph as the text-file substitute.
    for (const [file, icon] of [['config/options.json', 'fileJson'], ['docs/readme.txt', 'file'], ['art/picture.png', 'image']]) {
      await openFollowupFile(page, file);
      await expect(page.locator('.file-head').locator(`[data-icon="${icon}"]`)).toBeVisible();
      await expect(page.getByRole('tab', { name: file.split('/').pop()!, exact: true }).locator(`[data-icon="${icon}"]`)).toBeVisible();
    }
  });

  test('an image file shows the picture', async ({ page }) => {
    await startFileFollowups(page);
    await openFollowupFile(page, 'art/picture.png');
    const image = page.locator('.file-picture');
    await expect(image).toBeVisible();
    await expect(image).toHaveAttribute('alt', 'picture.png');
    await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBe(2);
    const pixels = await image.evaluate((img: HTMLImageElement) => {
      const canvas = document.createElement('canvas'); canvas.width = 2; canvas.height = 1;
      const context = canvas.getContext('2d')!; context.drawImage(img, 0, 0);
      return Array.from(context.getImageData(0, 0, 2, 1).data);
    });
    expect(pixels[3]).toBe(255); expect(pixels[7]).toBe(255);
    expect(pixels.slice(0, 3)).not.toEqual(pixels.slice(4, 7));
    await expectFileAccessible(page);
  });

  test('an edit by the conversation refreshes the open diff once', async ({ page }) => {
    const { engine, reads, edit } = await openFile(page, []);
    await expect.poll(() => engine.calls.filter(call => call.path.endsWith('/events')).length).toBeGreaterThan(1);
    await page.clock.install();
    const before = reads.filter(read => read === 'diff').length;
    edit();
    // The transport fixture publishes the edit before the renderer's refresh timer is advanced.
    await expect.poll(() => page.clock.runFor(100).then(() => page.locator('.file-diff-row[data-kind="add"] .file-text').textContent())).toContain('refreshed()');
    await page.clock.runFor(5000);
    expect(reads.filter(read => read === 'diff').length - before).toBe(1);
  });

  test('the toggle works by keyboard with a ring only on keyboard focus', async ({ page }) => {
    await openFile(page, []);
    const changes = page.locator('.file-head').getByRole('radio', { name: 'Changes' });
    const file = page.locator('.file-head').getByRole('radio', { name: 'File', exact: true });
    await file.click();
    const pointerShadow = await file.evaluate(el => getComputedStyle(el).boxShadow);
    await expect(file).toBeChecked();
    await expect(file).toHaveAttribute('tabindex', '0');
    await page.keyboard.press('ArrowLeft');
    await expect(changes).toBeFocused();
    await expect(changes).toBeChecked();
    const keyboardShadow = await changes.evaluate(el => getComputedStyle(el).boxShadow);
    expect(keyboardShadow).not.toBe(pointerShadow);
    expect(keyboardShadow).toContain('2px');
    expect(keyboardShadow).toContain('6px');
    await expect(changes).toHaveAttribute('tabindex', '0');
    await expect(file).toHaveAttribute('tabindex', '-1');
    await page.keyboard.press('ArrowRight');
    await expect(file).toBeFocused();
    await expect(file).toBeChecked();
    await page.keyboard.press('ArrowRight');
    await expect(changes).toBeFocused();
    await expect(changes).toBeChecked();
    await changes.click();
    expect(await changes.evaluate(el => getComputedStyle(el).boxShadow)).toBe(pointerShadow);
  });

  test('file tab at 320 and 600 px has no page overflow', async ({ page }) => {
    await openFile(page, []);
    for (const width of [320, 600]) {
      await page.setViewportSize({ width, height: 700 });
      await expectNoHorizontalOverflow(page);
      await expect(page.locator('.file-head').getByRole('radio', { name: 'Changes' })).toBeVisible();
      await expect(page.locator('.file-head').getByRole('button', { name: 'Open in', exact: true })).toBeVisible();
      const boxes = await page.locator('.file-head').evaluate(el => {
        const identity = el.querySelector('.file-id')!.getBoundingClientRect();
        const actions = el.querySelector('.file-actions')!.getBoundingClientRect();
        return { identityBottom: identity.bottom, actionsTop: actions.top, actionsRight: actions.right, viewport: innerWidth };
      });
      expect(boxes.actionsTop).toBeGreaterThanOrEqual(boxes.identityBottom);
      expect(boxes.actionsRight).toBeLessThanOrEqual(boxes.viewport);
      await expectFileAccessible(page);
    }
  });

  test('axe contract for the diff and whole file in Light and Dark', async ({ page }) => {
    await openFile(page, []);
    await expectFileAccessible(page);
    await page.locator('.file-head').getByRole('radio', { name: 'File', exact: true }).click();
    await expect(page.locator('.file-plain-row').last()).toContainText('fixedClock()');
    await expectFileAccessible(page);
  });

});

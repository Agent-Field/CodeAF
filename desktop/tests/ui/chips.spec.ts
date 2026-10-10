import { test, expect as baseExpect, type Locator, type Page } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openPage } from './support/shell-navigation';
import { openApp, send } from './support/conversation';
import { expectAccessible, tokenColorIn } from './contracts';

const expect = baseExpect.configure({ timeout: 2000 });

// C-CHIP-1..9 / C-COMP-3; PR-CHIP-1..8. Fixed dimensions are the measured design
// acceptance, independent of application tokens so changing a token cannot bless drift.
const assets = (page: Page) => page.getByRole('region', { name: 'Assets specimen', exact: true });
const files = (page: Page) => assets(page).locator('.asset-specimen-row').first();
const file = (page: Page) => files(page).locator('.file-chip').first();
const links = (page: Page) => assets(page).locator('.link-chip');
const model = (page: Page) => page.getByLabel('Model popover try-out').locator('.model-picker');

async function specimen(page: Page, theme: 'light' | 'dark') {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.emulateMedia({ colorScheme: theme });
  await installMockEngine(page, { initial: { entries: [], title: '' } });
  await page.goto('/');
  await openPage(page, 'Design system');
  await expect(assets(page)).toBeVisible();
}

async function rect(target: Locator) {
  return target.evaluate(element => {
    const { width, height } = element.getBoundingClientRect();
    return { width, height };
  });
}

async function color(target: Locator, token: string, property = 'color') {
  await expect.soft(target).toHaveCSS(property, await tokenColorIn(target, token));
}

async function pill(target: Locator, height: number, radius: number) {
  expect.soft((await rect(target)).height).toBe(height);
  await expect.soft(target).toHaveCSS('border-radius', `${radius}px`);
  await expect.soft(target).toHaveCSS('font-size', '12px');
}

async function mark(target: Locator, size: number, radius?: number) {
  expect.soft(await rect(target)).toEqual({ width: size, height: size });
  if (radius !== undefined) await expect.soft(target).toHaveCSS('border-radius', `${radius}px`);
}

async function keyboardFocus(page: Page, target: Locator) {
  // Moving from the next DOM stop with a real Tab establishes keyboard modality in both engines.
  await target.focus();
  await page.keyboard.press('Tab');
  await page.keyboard.press('Shift+Tab');
  await expect(target).toBeFocused();
  const expected = await target.evaluate(element => {
    const probe = document.createElement('span');
    probe.style.boxShadow = '0 0 0 2px var(--accent), 0 0 0 6px var(--accent-soft)';
    element.append(probe);
    const result = getComputedStyle(probe).boxShadow;
    probe.remove();
    return result;
  });
  await expect.soft(target).toHaveCSS('box-shadow', expected);
}

async function states(page: Page, target: Locator, hoverToken: string) {
  await target.scrollIntoViewIfNeeded();
  await page.mouse.move(0, 0);
  await target.evaluate(element => (element as HTMLElement).blur());
  const before = await target.evaluate(element => {
    const c = getComputedStyle(element);
    return { color: c.color, shadow: c.boxShadow, transform: c.transform, border: c.borderLeftWidth };
  });
  await target.hover();
  await color(target, hoverToken, 'background-color');
  await expect.soft(target).toHaveCSS('transition-duration', /0\.12s/);
  const hovered = await target.evaluate(element => {
    const c = getComputedStyle(element);
    return { color: c.color, shadow: c.boxShadow, transform: c.transform, border: c.borderLeftWidth };
  });
  expect.soft(hovered, 'Hover changes only the fill.').toEqual(before);
  const hoverPaint = await target.evaluate(element => {
    const c = getComputedStyle(element);
    return { background: c.backgroundColor, filter: c.filter };
  });
  await page.mouse.down();
  try {
    await expect.soft(target).toHaveCSS('transition-duration', /0\.08s/);
    await expect.soft.poll(() => target.evaluate(element => {
      const c = getComputedStyle(element);
      return { background: c.backgroundColor, filter: c.filter };
    }), { message: 'Press must darken beyond the hover fill.' }).not.toEqual(hoverPaint);
    await expect.soft(target).toHaveCSS('box-shadow', 'none');
  } finally {
    // Releasing outside avoids opening a preview or model menu while testing paint.
    await page.mouse.move(0, 0);
    await page.mouse.up();
  }
  await keyboardFocus(page, target);
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(`${theme}: conversation chips`, () => {
    test.beforeEach(async ({ page }) => specimen(page, theme));

    test('PR-CHIP-1/2: file geometry, typography and signed stats', async ({ page }) => {
      await file(page).scrollIntoViewIfNeeded();
      await pill(file(page), 20, 6);
      await color(file(page), 'field', 'background-color');
      await mark(file(page).locator(':scope > .app-icon'), 12);
      await color(file(page).locator(':scope > .app-icon'), 'ink-2');
      await expect.soft(file(page).locator('.file-chip-name')).toHaveCSS('font-weight', '500');
      await color(file(page).locator('.file-chip-name'), 'ink');
      await color(file(page).locator('.file-chip-dir'), 'ink-3');
      await expect.soft(file(page).locator('.file-chip-added')).toHaveText('+31');
      await expect.soft(file(page).locator('.file-chip-removed')).toHaveText('−4');
      await expect.soft(file(page).locator('.file-chip-stat')).toHaveCSS('font-size', '11px');
      await color(file(page).locator('.file-chip-added'), 'success');
      await color(file(page).locator('.file-chip-removed'), 'danger');
      // A file without engine counts must not acquire a manufactured zero statistic.
      await expect.soft(files(page).locator('.file-chip').nth(1).locator('.file-chip-stat')).toHaveCount(0);
    });

    test('PR-CHIP-3: file hover, press and keyboard-only ring', async ({ page }) => {
      await states(page, file(page), 'field-2');
    });

    test('PR-CHIP-4: missing and outside chips identify the refusal and cannot open', async ({ page }) => {
      for (const state of ['missing', 'outside'] as const) {
        const target = files(page).locator(`.file-chip[data-state="${state}"]`).first();
        const note = state === 'missing' ? 'not found' : 'outside this workspace';
        await expect(target).toHaveAttribute('aria-disabled', 'true');
        await expect.soft(target.locator('.file-chip-dir')).toHaveText(note);
        await expect.soft(target.locator('.app-icon')).toHaveAttribute('data-icon', state === 'missing' ? 'fileMissing' : 'fileLock');
        await color(target, 'ink-3');
        await color(target.locator('.app-icon'), 'ink-3');
        await pill(target, 20, 6);
        await target.click({ force: true });
        await expect.soft(page.getByRole('dialog', { name: /Preview/ })).toHaveCount(0);
      }
    });

    test('PR-CHIP-5/6: link tile, site hue, muted domain and small prose mark', async ({ page }) => {
      const target = links(page).filter({ hasText: 'asyncio, asynchronous I/O' }).first();
      await target.scrollIntoViewIfNeeded();
      await pill(target, 20, 6);
      await color(target, 'field', 'background-color');
      const tile = target.locator('.site-icon-mono');
      await mark(tile, 14, 4);
      const hue = await tile.getAttribute('data-hue');
      expect.soft(hue).toMatch(/^[1-6]$/);
      await color(tile, `site-icon-hue-${hue}`, 'background-color');
      await expect.soft(target.locator('.link-chip-title')).toHaveCSS('font-weight', '500');
      await expect.soft(target.locator('.link-chip-domain')).toHaveText('docs.python.org');
      await color(target.locator('.link-chip-domain'), 'ink-3');
      const textLink = assets(page).locator('.text-link').first();
      await expect(textLink).toBeVisible();
      await mark(textLink.locator('.site-icon'), 12, 3);
      await color(textLink, 'accent');
      // The fixture's fake engine has contacted GitHub only; every other site stays a monogram.
      await expect(assets(page).locator('a[href*="github.com"] .site-icon-favicon').first()).toHaveAttribute('src', /^data:image\//);
      await expect.soft(assets(page).locator('a:not([href*="github.com"]) .site-icon-favicon')).toHaveCount(0);
    });

    test('PR-CHIP-3/5: link hover, press and keyboard-only ring', async ({ page }) => {
      await states(page, links(page).filter({ hasText: 'asyncio, asynchronous I/O' }).first(), 'field-2');
    });

    test('PR-CHIP-8: pointer and keyboard menus retain actions, restrictions and focus', async ({ page }) => {
      for (const state of ['exists', 'missing', 'outside'] as const) {
        const target = files(page).locator(`.file-chip[data-state="${state}"]`).first();
        await target.click({ button: 'right', force: true });
        const menu = page.getByRole('menu');
        await expect(menu.getByRole('menuitem')).toHaveText(['Open in editor', /Reveal in (Finder|Files)/, 'Copy path', 'Copy relative path']);
        for (const label of ['Open in editor', 'Reveal in']) {
          const item = menu.getByRole('menuitem', { name: new RegExp(`^${label}`) });
          if (state === 'exists') await expect(item).toBeEnabled();
          else await expect(item).toBeDisabled();
        }
        await expect(menu.getByRole('menuitem', { name: 'Copy path', exact: true })).toBeEnabled();
        if (state === 'outside') await expect(menu.getByRole('menuitem', { name: 'Copy relative path', exact: true })).toBeDisabled();
        await page.keyboard.press('Escape');
        await expect(menu).toHaveCount(0);
        await expect(target).toBeFocused();
      }
      const target = links(page).first();
      await target.focus();
      await page.keyboard.press('Shift+F10');
      await expect(page.getByRole('menu').getByRole('menuitem')).toHaveText(['Copy link']);
      await expect(page.getByRole('menuitem', { name: 'Copy link', exact: true })).toBeFocused();
      await expectAccessible(page, '[role="menu"]');
      await page.keyboard.press('Escape');
      await expect(target).toBeFocused();
      await file(page).focus();
      await page.keyboard.press('Shift+F10');
      await expect(page.getByRole('menuitem', { name: 'Open in editor', exact: true })).toBeFocused();
    });

    test('PR-CHIP-7: model geometry, ink, chevron and keyboard menu selection', async ({ page }) => {
      await model(page).scrollIntoViewIfNeeded();
      await page.mouse.move(0, 0);
      await pill(model(page), 28, 8);
      await color(model(page), 'ink-2');
      await mark(model(page).locator('.app-icon'), 11);
      await keyboardFocus(page, model(page));
      await page.keyboard.press('Enter');
      const popover = page.getByRole('dialog', { name: 'Model', exact: true });
      await expect(popover).toBeVisible();
      await expect(popover.getByRole('radiogroup', { name: 'Models', exact: true }).locator('.model-popover-name')).toHaveText(['DeepSeek v4.1 Flash', 'DeepSeek v4.1 Pro', 'Claude Sonnet']);
      await popover.getByRole('radio', { name: 'Sonnet', exact: true }).click();
      await expect(popover).toHaveCount(0);
      await expect(model(page)).toHaveText('Sonnet');
      await expect(model(page)).toBeFocused();
    });

    test('PR-CHIP-7: model hover, press and keyboard-only ring', async ({ page }) => {
      await states(page, model(page), 'field');
    });

    test('PR-CHIP-1..8: axe scans Assets and composer model controls', async ({ page }) => {
      await assets(page).scrollIntoViewIfNeeded();
      // The shared contract exempts only the designer's explicit muted selectors from contrast.
      await expectAccessible(page, '.asset-specimen');
      await model(page).scrollIntoViewIfNeeded();
      await expectAccessible(page, '[aria-label="Model popover try-out"]');
      await model(page).click();
      await expectAccessible(page, '.model-popover');
    });
  });

  test(`${theme}: PR-CHIP-2/6 real path truncation and contacted-domain favicon use engine facts`, async ({ page }) => {
    await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
    const path = 'internal/payments/providers/stripe/webhooks/handler_test.go';
    await installMockEngine(page, {
      initial: { title: '', entries: [] },
      turns: [{ entries: [{ Role: 'assistant', Answer: true, Text: `Edited \`${path}\`.\n\nhttps://go.dev/blog/errors\n\nhttps://uncontacted.example/notes` }] }],
      files: { [path]: { mime: 'text/plain', dataBase64: Buffer.from('package webhooks\n').toString('base64') } },
    });
    const requested: string[] = [];
    const networkIcons: string[] = [];
    // This allowlist represents domains the engine has contacted; the renderer cannot extend it.
    const contacted = new Set(['go.dev']);
    const image = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
    page.on('request', request => { if (request.resourceType() === 'image' && /^https?:/.test(request.url())) networkIcons.push(request.url()); });
    await page.route('**/sessions/*/favicon?*', route => {
      const domain = new URL(route.request().url()).searchParams.get('domain')!;
      requested.push(domain);
      return route.fulfill({ json: contacted.has(domain) ? { dataUrl: image } : {} });
    });
    await openApp(page);
    await send(page, 'Show the contacted and uncontacted sites');
    const answer = page.locator('.answer-block');
    const long = answer.locator('.file-chip');
    await expect(long).toHaveAttribute('data-state', 'exists');
    await expect(long.locator('.file-chip-dir')).toHaveText(/^internal\/.*….*webhooks$/);
    await expect(long).toHaveAccessibleDescription(path);
    await expect(answer.locator('a[href="https://go.dev/blog/errors"] .site-icon-favicon')).toHaveAttribute('src', image);
    const unknown = answer.locator('a[href="https://uncontacted.example/notes"]');
    await expect(unknown.locator('.site-icon-mono')).toBeVisible();
    await expect(unknown.locator('img')).toHaveCount(0);
    expect(new Set(requested)).toEqual(new Set(['go.dev', 'uncontacted.example']));
    expect(networkIcons).toEqual([]);
  });
}

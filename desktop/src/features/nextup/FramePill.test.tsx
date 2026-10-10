import { expect, test } from '@playwright/test';
import design from '../../design/tokens.json' with { type: 'json' };

const url = (count: number) => `/?specimen=frame-pill&count=${count}`;
const pill = (page: import('@playwright/test').Page) => page.getByRole('button', { name: /need you elsewhere/ });

async function paint(page: import('@playwright/test').Page, name: string) {
  return page.evaluate((name) => {
    const probe = document.createElement('span');
    probe.style.backgroundColor = `var(--${name})`;
    document.body.append(probe);
    const value = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return value;
  }, name);
}

test.describe('frame pill', () => {
  for (const scheme of ['light', 'dark'] as const) {
    test(`${scheme}: matches the measured pill`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(url(5));
      const el = pill(page);
      await expect(el).toBeVisible();
      await expect(el).toHaveCSS('height', design.foundation['i2-pill-height']);
      await expect(el).toHaveCSS('border-radius', design.foundation['i2-pill-radius']);
      await expect(el).toHaveCSS('padding-left', design.foundation['i2-pill-pad-inline']);
      await expect(el).toHaveCSS('gap', design.foundation['i2-pill-gap']);
      await expect(el).toHaveCSS('background-color', await paint(page, 'tab-hover'));
      await expect(el).toHaveCSS('font-size', '12px');
      await expect(el).toHaveCSS('font-weight', '500');
      await expect(el).toContainText('5 need you');
      const glyph = el.locator('.frame-pill-glyph');
      const box = await glyph.boundingBox();
      expect([box?.width, box?.height]).toEqual([6, 6]);
      await expect(glyph).toHaveCSS('background-color', await paint(page, 'amber'));
      await expect(el.locator('.frame-pill-muted').first()).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
      await expect(el.locator('.frame-pill-muted').first()).toHaveCSS('font-weight', '400');
      expect(await el.locator('.frame-pill-muted').first().evaluate((n) => getComputedStyle(n).color)).toBe(
        await page.evaluate(() => { const p = document.createElement('i'); p.style.color = 'var(--ink-3)'; document.body.append(p); const c = getComputedStyle(p).color; p.remove(); return c; }),
      );
    });

    test(`${scheme}: hover changes the fill and never the words' colour`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(url(5));
      const el = pill(page);
      const ink = await el.evaluate((n) => getComputedStyle(n).color);
      await el.hover();
      await expect(el).toHaveCSS('background-color', await paint(page, 'field'));
      await expect(el).toHaveCSS('color', ink);
    });
  }

  test('reads aloud the count and the chord', async ({ page }) => {
    await page.goto(url(5));
    const label = await pill(page).getAttribute('aria-label');
    expect(label).toMatch(/^5 need you elsewhere\. Press (⌘J|Ctrl J)$/);
  });

  test('draws nothing at zero', async ({ page }) => {
    await page.goto(url(0));
    await expect(page.locator('.frame-pill')).toHaveCount(0);
  });

  test('click opens the queue', async ({ page }) => {
    await page.goto(url(2));
    await pill(page).click();
    await expect(page.locator('body')).toHaveAttribute('data-opened', 'queue');
  });
});

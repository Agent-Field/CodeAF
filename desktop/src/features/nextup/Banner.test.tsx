import { expect, test, type Page } from '@playwright/test';
import design from '../../design/tokens.json' with { type: 'json' };

const live = '/?specimen=nextup-banner';
const frozen = (pose: string) => `${live}&freeze=${pose}`;
const holdMs = Number.parseFloat(design.foundation['i2-banner-hold']);
const enterMs = Number.parseFloat(design.foundation['i2-banner-duration']);

async function paint(page: Page, name: string, property: 'color' | 'backgroundColor' | 'boxShadow') {
  return page.evaluate(({ name, property }) => {
    const probe = document.createElement('span');
    probe.style[property] = `var(--${name})`;
    document.body.append(probe);
    const value = getComputedStyle(probe)[property];
    probe.remove();
    return value;
  }, { name, property });
}

function banner(page: Page) {
  return page.getByRole('status');
}

test.describe('next up banner', () => {
  for (const scheme of ['light', 'dark'] as const) {
    test(`${scheme}: matches the measured card`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(frozen('rest'));
      const card = banner(page);
      await expect(card).toBeVisible();
      await expect(card).toHaveCSS('width', design.foundation['i2-banner-width']);
      await expect(card).toHaveCSS('box-sizing', 'content-box');
      await expect(card).toHaveCSS('padding-top', design.foundation['i2-banner-pad-block']);
      await expect(card).toHaveCSS('padding-right', design.foundation['i2-banner-pad-inline']);
      await expect(card).toHaveCSS('padding-bottom', design.foundation['i2-banner-pad-block']);
      await expect(card).toHaveCSS('padding-left', design.foundation['i2-banner-pad-inline']);
      await expect(card).toHaveCSS('border-radius', design.foundation['i2-banner-radius']);
      await expect(card).toHaveCSS('gap', '10px');
      await expect(card).toHaveCSS('background-color', await paint(page, 'surface', 'backgroundColor'));
      await expect(card).toHaveCSS('box-shadow', await paint(page, 'sh-2', 'boxShadow'));
      const box = await card.boundingBox();
      expect(Math.round(box!.width)).toBe(368);

      const glyph = card.locator('.nextup-banner-glyph');
      const glyphBox = await glyph.boundingBox();
      expect(Math.round(glyphBox!.width)).toBe(6);
      expect(Math.round(glyphBox!.height)).toBe(6);
      await expect(glyph).toHaveCSS('background-color', await paint(page, 'amber', 'backgroundColor'));

      const head = card.locator('.nextup-banner-head');
      await expect(head).toHaveText('Allow 3 git actions?');
      await expect(head).toHaveCSS('font-size', '12px');
      await expect(head).toHaveCSS('font-weight', '500');
      await expect(head).toHaveCSS('color', await paint(page, 'ink', 'color'));

      const detail = card.locator('.nextup-banner-detail');
      await expect(detail).toHaveText('Config parser · holding up 2 tasks');
      await expect(detail).toHaveCSS('color', await paint(page, 'ink-3', 'color'));
      await expect(detail).toHaveCSS('font-weight', design.foundation['font-weight-regular']);
      await expect(card.locator('.nextup-banner-copy')).toHaveCSS('gap', design.foundation['i2-banner-line-gap']);

      const mac = await page.evaluate(() => /Mac/.test(navigator.platform));
      const chord = card.locator('.nextup-banner-chord');
      await expect(chord).toHaveText(mac ? '⌘J' : 'Ctrl J');
      await expect(chord).toHaveCSS('color', await paint(page, 'ink-3', 'color'));
      await expect(chord).toHaveCSS('font-size', '12px');
    });
  }

  test('slides 8px out of the pill and fades over the base duration', async ({ page }) => {
    await page.goto(frozen('from-pill'));
    const card = banner(page);
    await expect(card).toHaveCSS('opacity', '0');
    const from = await card.evaluate(el => new DOMMatrix(getComputedStyle(el).transform).m42);
    expect(Math.round(from)).toBe(-8);
    await page.goto(frozen('rest'));
    await expect(banner(page)).toHaveCSS('opacity', '1');
    const duration = await banner(page).evaluate(el => getComputedStyle(el).transitionDuration);
    expect(duration.split(',').map(part => part.trim()).slice(0, 2)).toEqual(['0.2s', '0.2s']);
  });

  test('reduced motion fades and does not slide', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto(frozen('from-pill'));
    const card = banner(page);
    await expect(card).toHaveCSS('opacity', '0');
    const transform = await card.evaluate(el => getComputedStyle(el).transform);
    expect(transform === 'none' || transform === 'matrix(1, 0, 0, 1, 0, 0)').toBe(true);
    const duration = await card.evaluate(el => getComputedStyle(el).transitionDuration.split(',')[0].trim());
    expect(duration).toBe('0.2s');
  });

  test('blocking items only, and a missing second line draws nothing', async ({ page }) => {
    await page.goto(`${live}&blocking=0`);
    await expect(banner(page)).toHaveCount(0);
    await page.goto(`${live}&head=`);
    await expect(banner(page)).toHaveCount(0);
    await page.goto(`${live}&none=1`);
    await expect(banner(page)).toHaveCount(0);
    await page.goto(`${live}&detail=0&freeze=rest`);
    await expect(banner(page).locator('.nextup-banner-detail')).toHaveCount(0);
    await expect(banner(page).locator('.nextup-banner-head')).toHaveText('Allow 3 git actions?');
  });

  test('hover fills, press darkens, keyboard focus rings, a click does not', async ({ page, browserName }) => {
    await page.goto(frozen('rest'));
    const card = banner(page);
    await card.hover();
    await expect(card).toHaveCSS('background-color', await paint(page, 'field', 'backgroundColor'));
    await expect(card.locator('.nextup-banner-head')).toHaveCSS('color', await paint(page, 'ink', 'color'));
    await expect(card.locator('.nextup-banner-detail')).toHaveCSS('color', await paint(page, 'ink-3', 'color'));
    await page.mouse.down();
    const press = Number.parseFloat(design.foundation['brightness-press']);
    // The darkening takes dur-press, and engines round the settled brightness differently.
    await expect.poll(() => card.evaluate(el => {
      const match = /brightness\(([^)]+)\)/.exec(getComputedStyle(el).filter);
      return match ? Number(match[1]) : Number.NaN;
    })).toBeCloseTo(press, 2);
    await page.mouse.up();

    const rested = await paint(page, 'sh-2', 'boxShadow');
    await page.mouse.move(0, 0);
    await page.evaluate(() => { delete document.documentElement.dataset.input; });
    // A script focus does not match :focus-visible. Leaving and returning by keyboard does,
    // and the key clears the pointer mark. WebKit only tabs to a button with Option held.
    await card.focus();
    const tab = browserName === 'webkit' ? 'Alt+Tab' : 'Tab';
    const back = browserName === 'webkit' ? 'Alt+Shift+Tab' : 'Shift+Tab';
    await page.keyboard.press(tab);
    await page.keyboard.press(back);
    await expect(card).toBeFocused();
    const keyed = await card.evaluate(el => getComputedStyle(el).boxShadow);
    expect(keyed).not.toBe(rested);

    await card.click();
    await expect(card).toBeFocused();
    await expect(card).toHaveCSS('box-shadow', rested);
  });

  test('click starts Next up at that item and folds', async ({ page }) => {
    // The fold lasts one enter duration. A fake clock holds it open so a slow engine cannot
    // remove the card between the click and the assertions that read its folding pose.
    await page.clock.install();
    await page.goto(live);
    const card = banner(page);
    await expect(card).toBeAttached();
    await card.click();
    await expect(page.locator('.nextup-banner-specimen')).toHaveAttribute('data-opened', 'git-3');
    await expect(card).toHaveAttribute('data-phase', 'into-pill');
    await expect(card).toHaveAttribute('aria-live', 'off');
    await page.clock.fastForward(enterMs);
    await expect(card).toHaveCount(0);
  });

  test('folds after 4s, pauses on hover and focus, and announces once', async ({ page }) => {
    await page.clock.install();
    await page.goto(live);
    const card = banner(page);
    await expect(card).toBeAttached();
    const spoken = await card.innerText();
    expect(spoken).toContain('Allow 3 git actions?');
    expect(spoken).toContain('Config parser · holding up 2 tasks');
    await expect(card).toHaveAttribute('aria-live', 'polite');
    await expect(card).toHaveAttribute('aria-atomic', 'true');

    await page.clock.fastForward(holdMs - 1);
    await expect(card).toBeAttached();
    await page.clock.fastForward(1);
    await expect(card).toHaveAttribute('data-phase', 'into-pill');
    await expect(card).toHaveAttribute('aria-live', 'off');
    await page.clock.fastForward(enterMs);
    await expect(card).toHaveCount(0);
  });

  test('the hold pauses while hovered or focused and a new item starts it over', async ({ page }) => {
    await page.clock.install();
    await page.goto(live);
    const card = banner(page);
    await expect(card).toBeAttached();
    await page.clock.fastForward(holdMs / 2);
    await card.hover();
    await page.clock.fastForward(holdMs * 3);
    await expect(card).toBeAttached();
    expect(await card.innerText()).toContain('Allow 3 git actions?');
    await page.getByRole('button', { name: 'Another blocking question' }).hover();
    await page.clock.fastForward(holdMs / 2);
    await expect(card).toHaveAttribute('data-phase', 'into-pill');

    await page.goto(live);
    await page.clock.fastForward(holdMs / 2);
    await banner(page).focus();
    await page.clock.fastForward(holdMs * 3);
    await expect(banner(page)).toContainText('Allow 3 git actions?');
    await page.getByRole('button', { name: 'Another blocking question' }).focus();
    await page.clock.fastForward(holdMs);
    await expect(banner(page)).toHaveCount(0);

    await page.goto(live);
    await page.clock.fastForward(holdMs - 500);
    await page.getByRole('button', { name: 'Another blocking question' }).click();
    await expect(banner(page)).toContainText('Which pricing line should lead?');
    await page.mouse.move(0, 0);
    // The previous countdown does not carry over. A full hold from here folds the new card.
    await page.clock.fastForward(holdMs - 1);
    await expect(banner(page)).toBeAttached();
    await expect(banner(page)).toContainText('Which pricing line should lead?');
    await page.clock.fastForward(holdMs);
    await page.clock.fastForward(enterMs);
    await expect(banner(page)).toHaveCount(0);
  });

  test('a running update never replaces the banner, and a narrow window does not overflow', async ({ page }) => {
    await page.goto(frozen('rest'));
    await page.getByRole('button', { name: 'A running update' }).click();
    await expect(banner(page)).toHaveCount(0);
    await page.setViewportSize({ width: 320, height: 800 });
    await page.goto(frozen('rest'));
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
});

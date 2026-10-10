import { expect, test, type Page } from '@playwright/test';

async function ink(page: Page) {
  return page.getByTestId('ink3').evaluate(element => getComputedStyle(element).color);
}

test.describe('place status line', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Light' }).click();
  });

  test('deciding is the sparkle line under the title', async ({ page }) => {
    const stack = page.getByTestId('deciding');
    const title = stack.locator('.type-home-title');
    const line = stack.locator('.status-line');
    await expect(line).toHaveAttribute('data-mode', 'deciding');
    await expect(line.locator('.status-line-text')).toHaveText('Deciding automatically · 41 of 44 agreed this week');
    const glyph = line.locator('[data-icon="sparkle"]');
    await expect(glyph).toHaveCount(1);
    const measured = await line.evaluate(element => {
      const style = getComputedStyle(element);
      const icon = element.querySelector('[data-icon="sparkle"]');
      const iconBox = icon?.getBoundingClientRect();
      return {
        fontSize: style.fontSize,
        fontWeight: style.fontWeight,
        lineHeight: style.lineHeight,
        letterSpacing: style.letterSpacing,
        gap: style.gap,
        display: style.display,
        alignItems: style.alignItems,
        color: style.color,
        glyphColor: icon ? getComputedStyle(icon).color : '',
        glyphW: iconBox?.width ?? 0,
        glyphH: iconBox?.height ?? 0,
        tag: element.tagName,
      };
    });
    expect(measured.fontSize).toBe('12px');
    expect(measured.fontWeight).toBe('400');
    expect(measured.letterSpacing).toBe('normal');
    if (measured.lineHeight !== 'normal') expect(Number.parseFloat(measured.lineHeight)).toBeCloseTo(14, 0);
    expect(measured.gap).toBe('8px');
    expect(measured.display).toBe('flex');
    expect(measured.alignItems).toBe('center');
    expect(measured.tag).toBe('P');
    expect(measured.glyphW).toBeCloseTo(12, 0);
    expect(measured.glyphH).toBeCloseTo(12, 0);
    expect(measured.color).toBe(await ink(page));
    expect(measured.glyphColor).toBe(measured.color);
    const gap = await stack.evaluate(element => getComputedStyle(element).gap);
    expect(gap).toBe('10px');
    const titleBox = await title.boundingBox();
    const lineBox = await line.boundingBox();
    expect(titleBox).not.toBeNull();
    expect(lineBox).not.toBeNull();
    expect(lineBox!.y - (titleBox!.y + titleBox!.height)).toBeCloseTo(10, 0);
    expect(lineBox!.height).toBeCloseTo(14, 0);
  });

  test('learning hides the kind and always-ask has no count', async ({ page }) => {
    const learning = page.getByTestId('learning').locator('.status-line-text');
    await expect(learning).toHaveText('Learning · 14 of 20 agreed');
    await expect(learning).not.toContainText('shell');
    await expect(page.getByTestId('always').locator('.status-line-text')).toHaveText('Always asks you');
  });

  test('a place that has never been asked draws nothing', async ({ page }) => {
    await expect(page.getByTestId('none').locator('.status-line')).toHaveCount(0);
    await expect(page.getByTestId('missing').locator('.status-line')).toHaveCount(0);
  });

  test('a zero or impossible count keeps the mode word and drops the tally', async ({ page }) => {
    await expect(page.getByTestId('deciding-zero').locator('.status-line-text')).toHaveText('Deciding automatically');
    await expect(page.getByTestId('deciding-over').locator('.status-line-text')).toHaveText('Deciding automatically');
    await expect(page.getByTestId('learning-zero').locator('.status-line-text')).toHaveText('Learning');
    await expect(page.getByTestId('learning-missing').locator('.status-line-text')).toHaveText('Learning');
  });

  test('ink-3 follows the theme and the line is not a control', async ({ page }) => {
    const line = page.getByTestId('deciding').locator('.status-line');
    const light = await line.evaluate(element => getComputedStyle(element).color);
    expect(light).toBe(await ink(page));
    const fill = await line.evaluate(element => getComputedStyle(element).backgroundColor);
    await line.hover();
    const hovered = await line.evaluate(element => getComputedStyle(element).backgroundColor);
    expect(hovered).toBe(fill);
    await expect(line).not.toHaveAttribute('tabindex');
    await page.getByRole('button', { name: 'Dark' }).click();
    const dark = await line.evaluate(element => getComputedStyle(element).color);
    expect(dark).toBe(await ink(page));
    expect(dark).not.toBe(light);
  });
});

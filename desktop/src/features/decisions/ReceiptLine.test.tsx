import { expect, test, type Locator, type Page } from '@playwright/test';

const middle = '\u00b7';

async function color(page: Page, id: string) {
  return page.getByTestId(id).evaluate(element => getComputedStyle(element).color);
}

async function fill(page: Page, id: string) {
  return page.getByTestId(id).evaluate(element => getComputedStyle(element).backgroundColor);
}

function box(locator: Locator) {
  return locator.evaluate(element => {
    const rect = element.getBoundingClientRect();
    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
  });
}

test.describe('decision receipt line', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Light' }).click();
  });

  test('an automatic allowance is the sparkle line with the place and Why?', async ({ page }) => {
    const line = page.getByTestId('allowed').locator('.decision-receipt');
    await expect(line).toHaveAttribute('data-outcome', 'allowed');
    await expect(line).toHaveText(`Allowed automatically by Config parser ${middle} you always allow this here ${middle} Why?`);
    const open = line.getByRole('button', { name: /Allowed automatically by Config parser/ });
    const why = line.getByRole('button', { name: 'Why?' });
    const ink3 = await color(page, 'ink3');
    const ink2 = await color(page, 'ink2');
    const measured = await line.evaluate(element => {
      const style = getComputedStyle(element);
      return { fontSize: style.fontSize, fontWeight: style.fontWeight, color: style.color, display: style.display, alignItems: style.alignItems };
    });
    expect(measured.fontSize).toBe('12px');
    expect(measured.fontWeight).toBe('400');
    expect(measured.color).toBe(ink3);
    expect(measured.display).toBe('flex');
    expect(measured.alignItems).toBe('center');
    const actor = line.locator('.decision-receipt-actor');
    await expect(actor).toHaveText('Config parser');
    expect(await actor.evaluate(element => getComputedStyle(element).color)).toBe(ink2);
    expect(await actor.evaluate(element => getComputedStyle(element).fontWeight)).toBe('400');
    expect(await why.evaluate(element => getComputedStyle(element).color)).toBe(ink2);
    expect(await why.evaluate(element => getComputedStyle(element).fontSize)).toBe('12px');
    expect(await why.evaluate(element => getComputedStyle(element).fontWeight)).toBe('400');
    const glyph = open.locator('[data-icon="sparkle"]');
    const glyphBox = await box(glyph);
    const textBox = await box(open.locator('.decision-receipt-text'));
    expect(glyphBox.width).toBeCloseTo(12, 0);
    expect(glyphBox.height).toBeCloseTo(12, 0);
    expect(await glyph.evaluate(element => getComputedStyle(element).color)).toBe(ink3);
    expect(textBox.x - (glyphBox.x + glyphBox.width)).toBeCloseTo(8, 0);
    const openBox = await box(open);
    expect(openBox.height).toBeGreaterThan(10);
    expect(openBox.height).toBeLessThan(22);
    expect(await open.evaluate(element => getComputedStyle(element).transitionDuration)).toContain('0.12s');
  });

  test('the line opens the question and Why? does not', async ({ page }) => {
    const line = page.getByTestId('allowed').locator('.decision-receipt');
    await line.getByRole('button', { name: /Allowed automatically/ }).click();
    await expect(page.locator('#log')).toHaveText('open:q-allow');
    await line.getByRole('button', { name: 'Why?' }).click();
    await expect(page.locator('#log')).toHaveText('open:q-allow\nwhy:q-allow');
  });

  test('a missing reason drops its clause and a blank place draws nothing', async ({ page }) => {
    const bare = page.getByTestId('noreason').locator('.decision-receipt');
    await expect(bare).toHaveText(`Allowed automatically by Config parser ${middle} Why?`);
    await expect(page.getByTestId('blank').locator('.decision-receipt')).toHaveCount(0);
  });

  test('a policy denial uses the same line and the same ink', async ({ page }) => {
    const denied = page.getByTestId('denied').locator('.decision-receipt');
    const allowed = page.getByTestId('allowed').locator('.decision-receipt');
    await expect(denied).toHaveAttribute('data-outcome', 'denied');
    await expect(denied).toHaveText(`Not allowed by Security ${middle} this place does not allow it ${middle} Why?`);
    const ink3 = await color(page, 'ink3');
    const danger = await color(page, 'danger');
    const deniedColor = await denied.evaluate(element => getComputedStyle(element).color);
    const allowedColor = await allowed.evaluate(element => getComputedStyle(element).color);
    expect(deniedColor).toBe(ink3);
    expect(deniedColor).toBe(allowedColor);
    expect(deniedColor).not.toBe(danger);
    expect(await denied.locator('[data-icon="sparkle"]').evaluate(element => getComputedStyle(element).color)).toBe(ink3);
    expect(await denied.locator('.decision-receipt-actor').evaluate(element => getComputedStyle(element).color)).toBe(await color(page, 'ink2'));
    await denied.getByRole('button', { name: /Not allowed by Security/ }).click();
    await expect(page.locator('#log')).toHaveText('open:q-deny');
  });

  test('three actions fold into one line and open into their receipts', async ({ page }) => {
    const group = page.getByTestId('grouped').locator('.decision-receipt-group');
    const summary = group.getByRole('button', { name: /Did 3 things/ });
    await expect(summary).toHaveAttribute('aria-expanded', 'false');
    await expect(summary).toHaveText(`Did 3 things ${middle} held Launch post, asked Software, saved a note`);
    await expect(summary.locator('.decision-receipt-actor')).toHaveText(['held Launch post', 'asked Software', 'saved a note']);
    const ink2 = await color(page, 'ink2');
    const ink3 = await color(page, 'ink3');
    expect(await summary.locator('.decision-receipt-actor').first().evaluate(element => getComputedStyle(element).color)).toBe(ink2);
    expect(await summary.evaluate(element => getComputedStyle(element).color)).toBe(ink3);
    expect(await summary.evaluate(element => getComputedStyle(element).fontSize)).toBe('12px');
    expect(await summary.evaluate(element => getComputedStyle(element).gap)).toBe('8px');
    const spark = await box(summary.locator('[data-icon="sparkle"]'));
    const phrase = await box(summary.locator('.decision-receipt-text'));
    const chevron = summary.locator('[data-icon="chevron"]');
    const chevronBox = await box(chevron);
    expect(spark.width).toBeCloseTo(12, 0);
    expect(spark.height).toBeCloseTo(12, 0);
    expect(phrase.x - (spark.x + spark.width)).toBeCloseTo(8, 0);
    expect(chevronBox.width).toBeCloseTo(11, 0);
    expect(chevronBox.height).toBeCloseTo(11, 0);
    expect(chevronBox.x - (phrase.x + phrase.width)).toBeCloseTo(8, 0);
    expect(await chevron.evaluate(element => getComputedStyle(element).transform)).toBe('none');
    expect(await chevron.evaluate(element => getComputedStyle(element).color)).toBe(ink3);
    await expect(group.locator('.decision-receipt')).toHaveCount(0);

    await summary.click();
    await expect(summary).toHaveAttribute('aria-expanded', 'true');
    expect(await chevron.evaluate(element => getComputedStyle(element).transform)).not.toBe('none');
    const lines = group.locator('.decision-receipt');
    await expect(lines).toHaveCount(3);
    const summaryBox = await box(summary);
    const lineBoxes = await lines.evaluateAll(elements => elements.map(element => {
      const rect = element.getBoundingClientRect();
      return { y: rect.y, height: rect.height };
    }));
    expect(lineBoxes[0].y - (summaryBox.y + summaryBox.height)).toBeCloseTo(4, 0);
    expect(lineBoxes[1].y - (lineBoxes[0].y + lineBoxes[0].height)).toBeCloseTo(4, 0);
    expect(lineBoxes[2].y - (lineBoxes[1].y + lineBoxes[1].height)).toBeCloseTo(4, 0);
    expect(await group.evaluate(element => getComputedStyle(element).gap)).toBe('4px');

    await lines.nth(0).getByRole('button', { name: /Allowed automatically by Config parser/ }).click();
    await lines.nth(1).getByRole('button', { name: 'Why?' }).click();
    await expect(page.locator('#log')).toHaveText('open:q-hold\nwhy:q-ask');

    await summary.click();
    await expect(summary).toHaveAttribute('aria-expanded', 'false');
    await expect(group.locator('.decision-receipt')).toHaveCount(0);
  });

  test('one action is the receipt itself', async ({ page }) => {
    const host = page.getByTestId('single');
    await expect(host.locator('.decision-receipt-group')).toHaveCount(0);
    await expect(host.locator('.decision-receipt')).toHaveText(`Allowed automatically by Config parser ${middle} you always allow this here ${middle} Why?`);
    await expect(host.getByText('Did 1 thing')).toHaveCount(0);
  });

  test('hover fills, press darkens, and the ring is keyboard only', async ({ page }) => {
    const open = page.getByTestId('allowed').getByRole('button', { name: /Allowed automatically/ });
    const field = await fill(page, 'field');
    const field2 = await fill(page, 'field2');
    const ink3 = await color(page, 'ink3');
    expect(await open.evaluate(element => getComputedStyle(element).backgroundColor)).toBe('rgba(0, 0, 0, 0)');
    await open.hover();
    await expect(open).toHaveCSS('background-color', field);
    expect(await open.evaluate(element => getComputedStyle(element).color)).toBe(ink3);
    expect(await open.locator('.decision-receipt-actor').evaluate(element => getComputedStyle(element).color)).toBe(await color(page, 'ink2'));
    await page.mouse.down();
    await expect(open).toHaveCSS('background-color', field2);
    expect(await open.evaluate(element => getComputedStyle(element).transitionDuration)).toContain('0.08s');
    await page.mouse.up();

    expect(await open.evaluate(element => getComputedStyle(element).boxShadow)).toBe('none');
    await page.keyboard.press('Tab');
    const why = page.getByTestId('allowed').getByRole('button', { name: 'Why?' });
    await expect(why).toBeFocused();
    const shadow = await why.evaluate(element => getComputedStyle(element).boxShadow);
    expect(shadow).toMatch(/2px/);
    expect(shadow).toMatch(/6px/);
    expect(shadow).not.toBe('none');
  });

  test('ink follows the theme', async ({ page }) => {
    const line = page.getByTestId('allowed').locator('.decision-receipt');
    const light = await line.evaluate(element => getComputedStyle(element).color);
    expect(light).toBe(await color(page, 'ink3'));
    await page.getByRole('button', { name: 'Dark' }).click();
    const dark = await line.evaluate(element => getComputedStyle(element).color);
    expect(dark).toBe(await color(page, 'ink3'));
    expect(dark).not.toBe(light);
    expect(await line.locator('.decision-receipt-actor').evaluate(element => getComputedStyle(element).color)).toBe(await color(page, 'ink2'));
  });
});

import { test, expect, type Page } from '@playwright/test';
import { tokenColor } from './contracts';

for (const theme of ['light', 'dark']) {
 for (const width of [320, 1200]) {
  test(`PR-BTN-9: control is 28px and tray is 30px in ${theme} at ${width}px`, async ({ page }) => {
   await page.setViewportSize({ width, height: 560 });
   await page.goto('/tests/button-size/index.html');
   await page.locator('html').evaluate((element, value) => element.setAttribute('data-theme', value), theme);
   for (const variant of ['primary', 'raised', 'quiet', 'ghost', 'danger']) {
    for (const [label, height, size] of [['Default', 28, 'control'], ['Control', 28, 'control'], ['Tray', 30, 'tray']] as const) {
     const button = page.getByRole('button', { name: `${label} ${variant}`, exact: true });
     await expect(button).toHaveAttribute('data-size', size);
     await expect(button).toHaveCSS('min-height', `${height}px`);
     expect((await button.boundingBox())!.height).toBe(height);
     await expect(button).not.toHaveAttribute('size');
    }
   }
   await expect(page.getByRole('button', { name: 'Disabled tray' })).toBeDisabled();
   await expect(page.getByRole('button', { name: 'Loading tray' })).toBeDisabled();
   await expect(page.getByRole('button', { name: 'Loading tray' })).toHaveAttribute('aria-busy', 'true');
   const answer = page.getByRole('button', { name: 'Answer', exact: true });
   await answer.focus();
   await page.keyboard.press('Enter');
   await expect(page.getByRole('button', { name: 'Answered' })).toBeFocused();
   expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
 }
}

for (const theme of ['light', 'dark'] as const) {
 test(`PR-TAG-1: Suggested tag is transparent ink-3 in ${theme}`, async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 560 });
  await page.goto('/tests/tag-plain/index.html');
  await page.locator('html').evaluate((element, value) => element.setAttribute('data-theme', value), theme);
  const tag = page.getByText('Suggested', { exact: true });
  await expect(tag).toHaveAttribute('data-tone', 'plain');
  await expect(tag).toHaveCSS('color', await tokenColor(page, 'ink-3'));
  await expect(tag).toHaveCSS('font-size', '11px');
  await expect(tag).toHaveCSS('font-weight', '500');
  const paint = await tag.evaluate(element => {
   const style = getComputedStyle(element);
   return { backgroundColor: style.backgroundColor, padding: style.padding, borderRadius: style.borderRadius };
  });
  expect(paint.backgroundColor === 'rgba(0, 0, 0, 0)' || paint.backgroundColor === 'transparent').toBe(true);
  expect(paint.padding).toBe('0px');
  expect(paint.borderRadius).toBe('0px');
 });
}

async function paintPosition(page: Page, token: string) {
 return page.evaluate(name => {
  const probe = document.createElement('span');
  probe.style.backgroundPosition = `var(--${name}) 0px`;
  document.body.append(probe);
  const value = getComputedStyle(probe).backgroundPosition;
  probe.remove();
  return value;
 }, token);
}

for (const theme of ['light', 'dark'] as const) {
 for (const width of [320, 1200]) {
  test(`FD-MO-6/8: cf-shimmer 2.4s and cf-breathe 2.8s in ${theme} at ${width}px`, async ({ page }) => {
   await page.setViewportSize({ width, height: 560 });
   await page.goto('/tests/motion-primitives/index.html');
   await page.locator('html').evaluate((element, value) => element.setAttribute('data-theme', value), theme);
   const live = page.getByText('Running the parser tests', { exact: true });
   await expect(live).toHaveAttribute('data-shimmer', 'live');
   await expect(live).toHaveCSS('animation-name', 'cf-shimmer');
   await expect(live).toHaveCSS('animation-duration', '2.4s');
   await expect(live).toHaveCSS('animation-iteration-count', 'infinite');
   await expect(live).toHaveCSS('animation-timing-function', 'linear');
   await expect(live).toHaveCSS('background-clip', 'text');
   const liveColor = await live.evaluate(element => getComputedStyle(element).color);
   expect(liveColor === 'rgba(0, 0, 0, 0)' || liveColor === 'transparent').toBe(true);
   expect(await paintPosition(page, 'cf-shimmer-from')).toMatch(/^100%/);
   expect(await paintPosition(page, 'cf-shimmer-to')).toMatch(/-150%/);
   const frames = await page.evaluate(() => {
    const named: Record<string, string[]> = {};
    const visit = (rules: CSSRuleList) => {
     for (const rule of rules) {
      if (rule instanceof CSSKeyframesRule) named[rule.name] = [...rule.cssRules].map(frame => `${frame.keyText} ${frame.style.cssText}`);
      if (rule instanceof CSSGroupingRule && rule.cssRules) visit(rule.cssRules);
     }
    };
    for (const sheet of document.styleSheets) {
     try { visit(sheet.cssRules); } catch { /* a cross-origin sheet has no rules to read */ }
    }
    return named;
   });
   const shimmerFrames = frames['cf-shimmer']?.join('\n') ?? '';
   expect(shimmerFrames).toMatch(/\bfrom\b/);
   expect(shimmerFrames).toMatch(/\bto\b/);
   expect(shimmerFrames.includes('cf-shimmer-from') || shimmerFrames.includes('100%')).toBe(true);
   expect(shimmerFrames.includes('cf-shimmer-to') || shimmerFrames.includes('-150%')).toBe(true);
   const settled = page.getByText('Settled step', { exact: true });
   await expect(settled).toHaveAttribute('data-shimmer', 'still');
   await expect(settled).toHaveCSS('animation-name', 'none');
   const dot = page.locator('.cf-breathe');
   await expect(dot).toHaveCSS('animation-name', 'cf-breathe');
   await expect(dot).toHaveCSS('animation-duration', '2.8s');
   await expect(dot).toHaveCSS('animation-iteration-count', 'infinite');
   await expect(dot).toHaveCSS('animation-timing-function', 'ease-in-out');
   await expect(dot).toHaveCSS('width', '6px');
   await expect(dot).toHaveCSS('height', '6px');
   await expect(dot).toHaveCSS('background-color', await tokenColor(page, 'accent'));
   const breatheFrames = frames['cf-breathe']?.join('\n') ?? '';
   expect(breatheFrames.includes('breathe-from') || breatheFrames.includes('2px')).toBe(true);
   expect(breatheFrames.includes('breathe-to') || breatheFrames.includes('4.5px')).toBe(true);
   const box = (await dot.boundingBox())!;
   expect(box.width).toBe(6);
   expect(box.height).toBe(6);
   await expect(dot).toHaveAttribute('aria-hidden', 'true');
   expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });

  test(`FD-MO-9: reduced motion stops shimmer and breathe in ${theme} at ${width}px`, async ({ page }) => {
   await page.emulateMedia({ reducedMotion: 'reduce' });
   await page.setViewportSize({ width, height: 560 });
   await page.goto('/tests/motion-primitives/index.html');
   await page.locator('html').evaluate((element, value) => element.setAttribute('data-theme', value), theme);
   const live = page.getByText('Running the parser tests', { exact: true });
   await expect(live).toHaveCSS('animation-name', 'none');
   await expect(live).toHaveCSS('color', await tokenColor(page, 'ink-2'));
   const dot = page.locator('.cf-breathe');
   await expect(dot).toHaveCSS('animation-name', 'none');
   await expect(dot).toHaveCSS('box-shadow', 'none');
   const box = (await dot.boundingBox())!;
   expect(box.width).toBe(6);
   expect(box.height).toBe(6);
  });
 }
}

for (const theme of ['light', 'dark'] as const) {
 for (const width of [320, 1200]) {
  test(`PR-STOP-1: stop square is 10x10 r2 currentColor in ${theme} at ${width}px`, async ({ page }) => {
   await page.setViewportSize({ width, height: 560 });
   await page.goto('/tests/stop-glyph/index.html');
   await page.locator('.stop-glyph').first().waitFor();
   await page.locator('html').evaluate((element, value) => element.setAttribute('data-theme', value), theme);
   const declared = await page.evaluate(() => {
    for (const sheet of document.styleSheets) {
     let rules: CSSRuleList;
     try { rules = sheet.cssRules; } catch { continue; }
     for (const rule of rules) {
      if (rule instanceof CSSStyleRule && rule.selectorText === '.stop-glyph-mark') return rule.style.backgroundColor;
     }
    }
    return '';
   });
   expect(declared).toBe('currentcolor');
   const tones: Record<string, string> = {};
   for (const tone of ['ink', 'accent'] as const) {
    const row = page.locator(`[data-tone="${tone}"]`);
    const glyph = row.locator('.stop-glyph');
    const mark = row.locator('.stop-glyph-mark');
    const icon = row.locator('.app-icon');
    const size = tone === 'ink' ? 'md' : 'sm';
    await expect(glyph).toHaveAttribute('data-size', size);
    await expect(glyph).toHaveAttribute('aria-hidden', 'true');
    await expect(mark).toHaveCSS('width', '10px');
    await expect(mark).toHaveCSS('height', '10px');
    await expect(mark).toHaveCSS('border-radius', '2px');
    const markBox = (await mark.boundingBox())!;
    expect(markBox.width).toBe(10);
    expect(markBox.height).toBe(10);
    const glyphBox = (await glyph.boundingBox())!;
    const iconBox = (await icon.boundingBox())!;
    expect(glyphBox.width).toBe(iconBox.width);
    expect(glyphBox.height).toBe(iconBox.height);
    expect(glyphBox.width).toBe(size === 'md' ? 16 : 14);
    expect(glyphBox.height).toBe(size === 'md' ? 16 : 14);
    expect(markBox.x - glyphBox.x).toBe((glyphBox.width - markBox.width) / 2);
    expect(markBox.y - glyphBox.y).toBe((glyphBox.height - markBox.height) / 2);
    const ink = await glyph.evaluate(element => getComputedStyle(element).color);
    await expect(mark).toHaveCSS('background-color', ink);
    tones[tone] = ink;
   }
   expect(tones.ink).not.toBe(tones.accent);
   const flipped = theme === 'dark' ? 'light' : 'dark';
   await page.locator('html').evaluate((element, value) => element.setAttribute('data-theme', value), flipped);
   const flippedInk = await page.locator('[data-tone="ink"] .stop-glyph').evaluate(element => getComputedStyle(element).color);
   const flippedMark = await page.locator('[data-tone="ink"] .stop-glyph-mark').evaluate(element => getComputedStyle(element).backgroundColor);
   expect(flippedInk).not.toBe(tones.ink);
   expect(flippedMark).toBe(flippedInk);
   expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
 }
}

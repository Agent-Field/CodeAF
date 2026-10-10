import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { openApp, send } from './support/conversation';

const ANSWER = '**Full answer**\n\n```ts\nconst count = 42;\n```\n\n- Last line';

for (const theme of ['light', 'dark']) {
  test(`${theme}: user and folded menus support pointer, keyboard, source Copy and a background tab`, async ({ page }) => {
    await page.addInitScript(theme => {
      localStorage.setItem('codeaf-theme', theme);
      // Capturing writes proves the source text on browsers without clipboard-read permission.
      Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { (window as unknown as { copied: string }).copied = text; } } });
    }, theme);
    await installMockEngine(page, { initial: { entries: [], running: false }, turns: [{ entries: [{ Role: 'assistant', Text: ANSWER, Answer: true }] }] });
    await openApp(page);
    const words = 'Literal **question**\nSecond line';
    await send(page, words);
    await expect(page.getByText('Last line', { exact: true })).toBeVisible();
    const bubble = page.locator('.user-message-bubble');
    await bubble.click({ button: 'right' });
    await expect(page.getByRole('menuitem')).toHaveCount(1);
    await expect(page.getByRole('menuitem', { name: 'Copy', exact: true })).toBeVisible();
    await page.getByRole('menuitem', { name: 'Copy', exact: true }).click();
    await expect.poll(() => page.evaluate(() => (window as unknown as { copied: string }).copied)).toBe(words);
    await bubble.hover();
    await page.getByRole('button', { name: 'Copy message' }).click();
    await expect.poll(() => page.evaluate(() => (window as unknown as { copied: string }).copied)).toBe(words);
    for (const key of ['Shift+F10', 'ContextMenu']) {
      await bubble.focus();
      await page.keyboard.press(key);
      await expect(page.getByRole('menu', { name: 'Message actions' })).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(bubble).toBeFocused();
    }
    await page.getByRole('button', { name: 'Fold', exact: true }).click();
    const folded = page.getByRole('button', { name: 'Unfold', exact: true });
    await folded.click({ button: 'right' });
    await expect(page.getByRole('menuitem')).toHaveCount(2);
    await page.getByRole('menuitem', { name: 'Copy answer' }).click();
    await expect.poll(() => page.evaluate(() => (window as unknown as { copied: string }).copied)).toBe(ANSWER);
    await expect(folded).toBeVisible();
    for (const key of ['Shift+F10', 'ContextMenu']) {
      await folded.focus();
      await page.keyboard.press(key);
      await expect(page.getByRole('menu', { name: 'Folded turn actions' })).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(folded).toBeFocused();
    }
    const active = await page.getByRole('tab', { selected: true }).getAttribute('id');
    const count = await page.getByRole('tab').count();
    await folded.click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Open in new tab' }).click();
    await expect(page.getByRole('tab')).toHaveCount(count + 1);
    await expect(page.getByRole('tab', { selected: true })).toHaveAttribute('id', active!);
    await expect(folded).toBeVisible();
    await folded.click();
    await expect(page.getByText('Last line', { exact: true })).toBeVisible();
  });
}

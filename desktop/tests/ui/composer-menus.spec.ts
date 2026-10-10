import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { message, openApp, posts, send } from './support/conversation';

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: plain-text paste replaces the selection without a card or attachment`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    const text = Array.from({ length: 20 }, (_, i) => `line ${i}`).join('\n');
    await page.addInitScript(value => {
      Object.defineProperty(navigator.clipboard, 'readText', { value: async () => value });
    }, text);
    const engine = await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
    await openApp(page);
    const field = message(page);
    await field.fill('before REPLACE after');
    await field.evaluate(el => (el as HTMLTextAreaElement).setSelectionRange(7, 14));
    await field.press('Shift+F10');
    await page.getByRole('menuitem', { name: 'Paste as plain text' }).click();
    await expect(field).toHaveValue(`before ${text} after`);
    await expect(field).toBeFocused();
    expect(await field.evaluate(el => (el as HTMLTextAreaElement).selectionStart)).toBe(7 + text.length);
    await expect(page.getByText('Pasted text', { exact: true })).toHaveCount(0);
    await expect(page.getByRole('list', { name: 'Attachments' })).toHaveCount(0);
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect.poll(() => posts(engine, '/turn').length).toBe(1);
    expect(posts(engine, '/turn')[0].body).toMatchObject({ text: `before ${text} after` });
  });

  test(`${theme}: Queue instead is disabled idle and stopped-empty, and queues running text`, async ({ page }) => {
    const engine = await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' }, manual: true });
    await page.emulateMedia({ colorScheme: theme });
    await openApp(page);
    await message(page).fill('Start');
    await page.getByRole('button', { name: 'Send', exact: true }).click({ button: 'right' });
    const item = page.getByRole('menuitem', { name: /Queue instead/ });
    await expect(item).toHaveAttribute('aria-disabled', 'true');
    await expect(item.locator('.keyboard-shortcut')).toHaveText(/⌥↵|Alt Enter/);
    await page.keyboard.press('Escape');
    await send(page, 'Start');
    await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Stop', exact: true }).click({ button: 'right' });
    await expect(item).toHaveAttribute('aria-disabled', 'true');
    await page.keyboard.press('Escape');
    expect(posts(engine, '/stop')).toHaveLength(0);
    await page.getByTestId('composer-file-input').setInputFiles([
      { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('notes') },
    ]);
    await page.getByRole('button', { name: 'Steer', exact: true }).click({ button: 'right' });
    await expect(item).toHaveAttribute('aria-disabled', 'true');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Remove notes.txt', exact: true }).click();
    await message(page).fill('   ');
    await page.getByRole('button', { name: 'Stop', exact: true }).click({ button: 'right' });
    await expect(item).toHaveAttribute('aria-disabled', 'true');
    await page.keyboard.press('Escape');
    await message(page).fill('Later');
    await page.getByRole('button', { name: 'Steer', exact: true }).click({ button: 'right' });
    await expect(item).not.toHaveAttribute('aria-disabled', 'true');
    await item.click();
    await expect.poll(() => engine.snapshot().queue?.map(entry => entry.text)).toEqual(['Later']);
    expect(posts(engine, '/turn').at(-1)?.body).toMatchObject({ text: 'Later', mode: 'queue' });
    await expect(message(page)).toHaveValue('');
    expect(engine.snapshot().running).toBe(true);
  });
}

test('keyboard opens the message menu; denied clipboard access preserves the draft', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, 'readText', { value: async () => { throw new Error('denied'); } });
  });
  await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
  await openApp(page);
  await message(page).fill('Keep this');
  await message(page).press('Shift+F10');
  await expect(page.getByRole('menuitem', { name: 'Paste as plain text' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('status').filter({ hasText: 'Clipboard text could not be read' })).toBeVisible();
  await expect(message(page)).toHaveValue('Keep this');
});

test('right-click paste inserts clipboard text at the current caret', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, 'readText', { value: async () => 'clipboard' });
  });
  await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
  await openApp(page);
  await message(page).click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Paste as plain text' }).click();
  await expect(message(page)).toHaveValue('clipboard');
  // Replacing identical text still restores focus and puts the caret after the paste.
  await message(page).evaluate(el => (el as HTMLTextAreaElement).setSelectionRange(0, 9));
  await message(page).press('Shift+F10');
  await page.getByRole('menuitem', { name: 'Paste as plain text' }).click();
  await expect(message(page)).toBeFocused();
  expect(await message(page).evaluate(el => (el as HTMLTextAreaElement).selectionStart)).toBe(9);
});

test('a delayed clipboard read cannot overwrite newer typing', async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator.clipboard, 'readText', { value: () => new Promise<string>(resolve => {
      (window as typeof window & { finishPaste: () => void }).finishPaste = () => resolve('clipboard');
    }) });
  });
  await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
  await openApp(page);
  await message(page).fill('Before');
  await message(page).press('Shift+F10');
  await page.getByRole('menuitem', { name: 'Paste as plain text' }).click();
  await message(page).fill('Newer draft');
  await page.evaluate(() => (window as typeof window & { finishPaste: () => void }).finishPaste());
  await expect(message(page)).toHaveValue('Newer draft');
});

for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: composer menus fit narrow and short windows with reduced motion`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
    await openApp(page);
    for (const width of [320, 480, 600, 800, 1200]) {
      await page.setViewportSize({ width, height: 560 });
      await message(page).fill('Draft');
      await message(page).press('Shift+F10');
      const menu = page.getByRole('menu', { name: 'Message menu' });
      await expect(menu).toBeVisible();
      // Collision placement settles after the menu's first measured render.
      await expect.poll(async () => {
        const box = await menu.boundingBox();
        return !!box && box.x >= 0 && box.x + box.width <= width && box.y + box.height <= 560;
      }).toBe(true);
      expect(await menu.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
      await page.keyboard.press('Escape');
      await page.getByRole('button', { name: 'Send', exact: true }).click({ button: 'right' });
      await expect(page.getByRole('menuitem', { name: /Queue instead/ })).toBeVisible();
      const sendMenu = page.getByRole('menu', { name: 'Send menu' });
      await expect.poll(async () => {
        const box = await sendMenu.boundingBox();
        return !!box && box.x >= 0 && box.x + box.width <= width && box.y + box.height <= 560;
      }).toBe(true);
      expect(await sendMenu.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
      await page.keyboard.press('Escape');
    }
  });
}

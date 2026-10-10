import { test, expect } from '@playwright/test';
import { installMockEngine, MODEL, GLM } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { message, openApp, posts, send, expectNoHorizontalOverflow } from './support/conversation';
import { expectAccessible } from './contracts';

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
    await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible();
    await expect(field).toHaveValue('');
    await field.click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Paste as plain text' }).click();
    await expect(field).toHaveValue(text);
    await expect(page.getByText('Pasted text', { exact: true })).toHaveCount(0);
    await expect(page.getByRole('list', { name: 'Attachments' })).toHaveCount(0);
    await field.press('Enter');
    await expect.poll(() => posts(engine, '/turn').length).toBe(2);
    expect(posts(engine, '/turn')[1].body).toMatchObject({ text });
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
      await expectAccessible(page);
      await page.keyboard.press('Escape');
      await page.getByRole('button', { name: 'Send', exact: true }).click({ button: 'right' });
      await expect(page.getByRole('menuitem', { name: /Queue instead/ })).toBeVisible();
      const sendMenu = page.getByRole('menu', { name: 'Send menu' });
      await expect.poll(async () => {
        const box = await sendMenu.boundingBox();
        return !!box && box.x >= 0 && box.x + box.width <= width && box.y + box.height <= 560;
      }).toBe(true);
      expect(await sendMenu.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
      await expectAccessible(page);
      await page.keyboard.press('Escape');
    }
  });
}

// CV-203 keeps recall local: looking up the latest user message must never submit it.
for (const theme of ['light', 'dark'] as const) {
  test(`${theme}: empty Up recalls the latest message, preserves typing, and Escape blurs`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    const engine = await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
    await openApp(page);
    const field = message(page);
    await field.press('ArrowUp');
    await expect(field).toHaveValue('');
    await send(page, 'Earlier request');
    await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible();
    await send(page, 'Latest request');
    await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible();
    await expect(field).toHaveValue('');
    await field.press('ArrowUp');
    await expect(field).toHaveValue('Latest request');
    await expect(field).toBeFocused();
    await field.fill('Keep my draft');
    await field.press('ArrowUp');
    await expect(field).toHaveValue('Keep my draft');
    await field.press('Escape');
    await expect(field).not.toBeFocused();
    await expect(field).toHaveValue('Keep my draft');
    expect(posts(engine, '/turn').map(call => call.body.text)).toEqual(['Earlier request', 'Latest request']);
  });
}

// CV-225 / Q31, CV-227 and CV-288 use the real conversation, not the model specimen.
// The pins response is a profile fixture; the shared engine still handles role saves and turns.
const CUSTOM_MODEL = 'moonshotai/kimi-k3';
for (const platform of ['Linux x86_64', 'Win32']) {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [320, 600, 1200]) {
      test(`${platform}, ${theme}, ${width}px: custom pin, next-turn model, composer and turn keys`, async ({ page }) => {
        await page.setViewportSize({ width, height: 560 });
        await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
        await page.addInitScript(value => Object.defineProperty(navigator, 'platform', { value }), platform);
        const engine = await installMockEngine(page, {
          ...plainReply(), initial: { entries: [], title: '' }, manual: true,
          models: [
            { id: MODEL, name: 'DeepSeek V4.1 Flash' },
            { id: GLM, name: 'GLM 5.3' },
            { id: CUSTOM_MODEL, name: 'Kimi K3' },
          ],
        });
        await page.route('**/api/engine/models/pinned', route => route.fulfill({ json: {
          pinned: [
            { id: MODEL, label: 'DS Flash' },
            { id: GLM, label: 'GLM 5.3' },
            { id: CUSTOM_MODEL, label: 'kimi-k3' },
          ], chosen: true,
        } }));
        await openApp(page);
        const field = message(page);
        await field.fill('First line');
        await field.press('End');
        await field.press('Shift+Enter');
        await field.press('a');
        await expect(field).toHaveValue('First line\na');
        expect(posts(engine, '/turn')).toHaveLength(0);
        await field.press('Enter');
        await expect.poll(() => engine.turnModels).toEqual([MODEL]);
        await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible();
        const chip = page.getByRole('button', { name: /^Model:/ });
        await expect(chip).toHaveText('DS Flash');
        await field.press('Control+/');
        const picker = page.getByRole('dialog', { name: 'Model', exact: true });
        await expect(picker).toBeVisible();
        const pins = picker.getByRole('radiogroup', { name: 'Pinned models', exact: true });
        await expect(pins.getByRole('radio')).toHaveText(['DS Flash', 'GLM 5.3', 'kimi-k3']);
        await expect(picker.locator('kbd')).toHaveText(['Ctrl Alt 1', 'Ctrl Alt 2', 'Ctrl Alt 3']);
        await expectAccessible(page);
        await pins.getByRole('radio', { name: 'kimi-k3', exact: true }).focus();
        await page.keyboard.press('Enter');
        await expect(picker).toHaveCount(0);
        await expect(chip).toHaveText('kimi-k3');
        await expect(chip).toBeFocused();
        await expect.poll(() => engine.calls.filter(call => call.method === 'PUT' && call.path.endsWith('/models/roles/conversation')).at(-1)?.body.model).toBe(CUSTOM_MODEL);
        await field.fill('Queued with Alt Enter');
        await expect(page.getByRole('button', { name: 'Queue Alt Enter', exact: true })).toBeVisible();
        await field.press('Alt+Enter');
        await expect.poll(() => engine.snapshot().queue?.map(row => row.text)).toEqual(['Queued with Alt Enter']);
        expect(posts(engine, '/turn').at(-1)?.body).toMatchObject({ text: 'Queued with Alt Enter', mode: 'queue' });
        await expect(field).toHaveValue('');
        await field.fill('Steered with Enter');
        await field.press('Enter');
        await expect.poll(() => posts(engine, '/turn').length).toBe(3);
        expect(posts(engine, '/turn').at(-1)?.body).toMatchObject({ text: 'Steered with Enter', mode: 'steer' });
        // The bridge resolves the saved Conversation role at /turn; the wire body contains text and mode.
        expect(engine.turnModels).toEqual([MODEL, CUSTOM_MODEL, CUSTOM_MODEL]);
        await expect(field).toHaveValue('');
        await field.press('Control+Alt+2');
        await expect(chip).toHaveText('GLM 5.3');
        await expect.poll(() => engine.snapshot().model).toBe(GLM);
        await field.press('Control+Alt+3');
        await expect(chip).toHaveText('kimi-k3');
        await expect.poll(() => engine.snapshot().model).toBe(CUSTOM_MODEL);
        await page.getByRole('button', { name: 'Stop', exact: true }).click();
        await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible();
        await field.fill('Next submitted message');
        await field.press('Enter');
        await expect.poll(() => engine.turnModels).toEqual([MODEL, CUSTOM_MODEL, CUSTOM_MODEL, CUSTOM_MODEL]);
        expect(posts(engine, '/turn').at(-1)?.body).toMatchObject({ text: 'Next submitted message', mode: 'submit' });
        // Multiple real snapshot turns leave distinct scroll targets even when older answers fold.
        engine.update({ running: false, queue: [], entries: [
          ...Array.from({ length: 6 }, (_, i) => [
            { Role: 'user', Text: `Request ${i}` },
            { Role: 'assistant', Text: `Answer ${i}`, Answer: true },
          ]).flat(),
          { Role: 'user', Text: 'Latest request' },
          { Role: 'assistant', Text: Array.from({ length: 40 }, (_, i) => `Latest paragraph ${i}.`).join('\n\n'), Answer: true },
        ] });
        await expect(page.getByText('Latest paragraph 39.', { exact: true })).toBeVisible();
        await field.press('Escape');
        await expect(field).not.toBeFocused();
        const scroll = page.locator('.conversation-scroll');
        const position = () => scroll.evaluate(el => el.scrollTop);
        await expect.poll(() => scroll.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThanOrEqual(48);
        const bottom = await position();
        await page.keyboard.press('Control+ArrowUp');
        await expect.poll(position).toBeLessThan(bottom);
        const previous = await position();
        await page.keyboard.press('Control+ArrowUp');
        await expect.poll(position).toBeLessThan(previous);
        const earlier = await position();
        await page.keyboard.press('Control+ArrowDown');
        await expect.poll(position).toBeGreaterThan(earlier);
        await page.keyboard.press('Escape');
        await expect.poll(() => scroll.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop)).toBeLessThanOrEqual(48);
        await expectNoHorizontalOverflow(page);
        await expectAccessible(page);
      });
    }
  }
}

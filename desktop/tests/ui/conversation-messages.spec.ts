import { test, expect, type Locator, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { expectNoHorizontalOverflow, message, openApp } from './support/conversation';

const LINES = 12;
const PASTE_LINES = 214;
const typed = Array.from({ length: LINES }, (_, index) => `Clamp line ${index + 1} stays whole.`).join('\n');
const pastedBody = Array.from({ length: PASTE_LINES }, (_, index) => `paste line ${index + 1}`).join('\n');
const LONG = 'x'.repeat(240);
const ANSWER = [
  'Here is the patch.',
  '',
  '```ts',
  'const short = 1;',
  '```',
  '',
  '```go',
  `const veryLongIdentifier = "${LONG}";`,
  '```',
].join('\n');
const WIDTHS = [320, 600, 1200] as const;

function paste(field: Locator, text: string) {
  return field.evaluate((el, value) => {
    const data = new DataTransfer();
    data.setData('text/plain', value);
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  }, text);
}

function copied(page: Page) {
  return page.evaluate(() => (window as unknown as { copied?: string }).copied ?? '');
}

function sameSessionAttaches(engine: MockEngine) {
  return engine.calls.filter(call => call.method === 'POST' && call.path.endsWith('/sessions') && call.body.sessionFile === 'mock-session-1.jsonl').length;
}

async function maskImage(locator: Locator) {
  return locator.evaluate(el => {
    const style = getComputedStyle(el);
    const mask = style.maskImage;
    return mask && mask !== 'none' ? mask : style.getPropertyValue('-webkit-mask-image');
  });
}

/** A recorded turn: a clamped bubble with a paste card, and an answer with a short and a long code block. */
async function openRich(page: Page): Promise<MockEngine> {
  const engine = await installMockEngine(page, {
    initial: { entries: [], running: false },
    turns: [{ entries: [{ Role: 'assistant', Text: ANSWER, Answer: true }], patch: { title: 'Message checks' } }],
  });
  await openApp(page);
  await paste(message(page), pastedBody);
  await message(page).fill(typed);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Show more', exact: true })).toBeVisible();
  await expect(page.getByText('Here is the patch.', { exact: true })).toBeVisible();
  return engine;
}

for (const theme of ['light', 'dark'] as const) {
  test.describe(`conversation messages · ${theme}`, () => {
    test.beforeEach(async ({ page }) => {
      await page.addInitScript(themeName => {
        localStorage.setItem('codeaf-theme', themeName);
        // The write is what the test reads. Clipboard permission is not required, and a refused write must not look copied.
        Object.defineProperty(navigator, 'clipboard', {
          configurable: true,
          value: { writeText: async (text: string) => { (window as unknown as { copied: string }).copied = text; } },
        });
      }, theme);
      await page.emulateMedia({ colorScheme: theme });
    });

    test('a long bubble clamps at 8 lines, a paste card names its size, and Copy appears on hover and keyboard', async ({ page }) => {
      await openRich(page);
      const bubble = page.locator('.user-message-bubble');
      const text = page.locator('.user-message-text');
      const actions = page.locator('.user-message-actions');
      const copy = page.getByRole('button', { name: 'Copy message', exact: true });

      await expect(bubble.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0);
      await expect(bubble.getByRole('button', { name: 'Remove pasted text' })).toHaveCount(0);
      const card = bubble.locator('.paste-card[data-variant="sent"]');
      await expect(card.getByText('Pasted text', { exact: true })).toBeVisible();
      await expect(card.getByText('214 lines', { exact: true })).toBeVisible();
      const preview = card.locator('.paste-card-preview');
      await expect(preview).toHaveCSS('height', '48px');
      await expect(preview).toContainText('paste line 1');
      await expect(preview).not.toContainText('paste line 4');
      expect(await maskImage(preview)).toContain('25%');
      expect(await preview.evaluate(el => getComputedStyle(el).getPropertyValue('--paste-sent-fade').trim())).toBe('25%');

      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: 800 });
        await expect(text).toHaveAttribute('data-clamped', 'true');
        await expect(text).toHaveAttribute('data-faded', 'true');
        const clamp = await text.evaluate(el => {
          const style = getComputedStyle(el);
          const lines = Number(style.getPropertyValue('--chat-user-clamp-lines'));
          const line = parseFloat(style.lineHeight);
          return { lines, max: parseFloat(style.maxHeight), expected: lines * line, overflow: el.scrollHeight > el.clientHeight + 1 };
        });
        expect(clamp.lines).toBe(8);
        expect(Math.abs(clamp.max - clamp.expected)).toBeLessThan(1);
        expect(clamp.overflow).toBe(true);
        expect(await maskImage(text)).toContain('45%');
        const hidden = await text.evaluate(el => {
          const node = el.firstChild;
          if (!node) return false;
          const value = el.textContent ?? '';
          const at = value.lastIndexOf('Clamp line 12');
          const range = document.createRange();
          range.setStart(node, at);
          range.setEnd(node, at + 'Clamp line 12'.length);
          return range.getBoundingClientRect().top >= el.getBoundingClientRect().bottom - 1;
        });
        expect(hidden).toBe(true);
        await expectNoHorizontalOverflow(page);
      }

      await page.getByRole('button', { name: 'Show more', exact: true }).click();
      await expect(page.getByRole('button', { name: 'Show less', exact: true })).toHaveAttribute('aria-expanded', 'true');
      await expect(text).toHaveAttribute('data-clamped', 'false');
      await expect(text).not.toHaveAttribute('data-faded');
      await expect(text).toHaveCSS('max-height', 'none');
      expect(await maskImage(text)).toBe('none');
      const shown = await text.evaluate(el => {
        const node = el.firstChild;
        const value = el.textContent ?? '';
        const at = value.lastIndexOf('Clamp line 12');
        const range = document.createRange();
        range.setStart(node!, at);
        range.setEnd(node!, at + 'Clamp line 12'.length);
        const line = range.getBoundingClientRect();
        const box = el.getBoundingClientRect();
        return line.top >= box.top - 1 && line.bottom <= box.bottom + 1;
      });
      expect(shown).toBe(true);
      await page.getByRole('button', { name: 'Show less', exact: true }).click();
      await expect(page.getByRole('button', { name: 'Show more', exact: true })).toHaveAttribute('aria-expanded', 'false');

      // Show more keeps keyboard focus, and focus inside the message is enough to reveal Copy.
      await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); });
      await page.mouse.move(0, 0);
      await expect(actions).toHaveCSS('opacity', '0');
      await expect(actions).toHaveCSS('transition-duration', '0.12s');
      await bubble.hover();
      await expect(actions).toHaveCSS('opacity', '1');
      await expect(copy).toHaveCSS('width', '26px');
      await expect(copy).toHaveCSS('height', '26px');
      await page.mouse.move(0, 0);
      await bubble.focus();
      await expect(actions).toHaveCSS('opacity', '1');
      await page.keyboard.press('Tab');
      await page.keyboard.press('Tab');
      await expect(copy).toBeFocused();
      await expect(copy).toHaveCSS('box-shadow', /0px 0px 0px 2px/);
      await page.keyboard.press('Enter');
      await expect(copy.locator('[data-icon="check"]')).toBeVisible();
      await expect(page.locator('.user-message-actions .copy-button-status')).toHaveText('Copied');
      expect(await copied(page)).toBe(typed);
      expect(await copied(page)).not.toContain('paste line 1');
      expect(await copied(page)).not.toContain('<pasted-text');

      await bubble.click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Copy', exact: true }).click();
      expect(await copied(page)).toBe(typed);
      for (const key of ['Shift+F10', 'ContextMenu']) {
        await bubble.focus();
        await page.keyboard.press(key);
        await expect(page.getByRole('menu', { name: 'Message actions' })).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(bubble).toBeFocused();
      }
      await expectAccessible(page);
      await page.setViewportSize({ width: 320, height: 800 });
      await expectAccessible(page);
    });

    test('a code block copies from its head and fades the right edge only while more remains', async ({ page }) => {
      await openRich(page);
      const long = page.getByRole('region', { name: 'Code block' }).filter({ hasText: 'veryLongIdentifier' });
      const short = page.getByRole('region', { name: 'Code block' }).filter({ hasText: 'const short = 1;' });
      const longBlock = page.locator('.markdown-code').filter({ hasText: 'veryLongIdentifier' });
      const copy = longBlock.getByRole('button', { name: 'Copy code', exact: true });

      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: 800 });
        await expect(long).toHaveAttribute('data-more', 'true');
        expect(await maskImage(long)).toMatch(/24px/);
        await expect(short).toHaveAttribute('data-more', 'false');
        expect(await maskImage(short)).toBe('none');
        await expectNoHorizontalOverflow(page);
      }

      await page.setViewportSize({ width: 320, height: 800 });
      await long.evaluate(el => { el.scrollLeft = 0; });
      await long.focus();
      await page.keyboard.press('ArrowRight');
      await expect.poll(() => long.evaluate(el => el.scrollLeft)).toBeGreaterThan(0);
      await expect(long).toHaveAttribute('data-more', 'true');
      await long.evaluate(el => { el.scrollLeft = el.scrollWidth; });
      await expect(long).toHaveAttribute('data-more', 'false');
      expect(await maskImage(long)).toBe('none');
      await long.evaluate(el => { el.scrollLeft = 0; });
      await expect(long).toHaveAttribute('data-more', 'true');

      await expect(copy).toHaveCSS('width', '24px');
      await expect(copy).toHaveCSS('height', '24px');
      await copy.click();
      await expect(copy.locator('[data-icon="check"]')).toBeVisible();
      await expect(longBlock.locator('.copy-button-status')).toHaveText('Copied');
      await expect(longBlock.locator('.copy-button-status')).toBeVisible();
      expect(await copied(page)).toContain('veryLongIdentifier');
      expect(await copied(page)).toContain(LONG);
    });

    test('a folded turn copies the answer and opens the same session in a background tab', async ({ page }) => {
      const engine = await openRich(page);
      await page.getByRole('button', { name: 'Fold', exact: true }).click();
      const folded = page.getByRole('button', { name: 'Unfold', exact: true });
      await folded.click({ button: 'right' });
      await expect(page.getByRole('menuitem', { name: 'Copy answer', exact: true })).toBeVisible();
      await expect(page.getByRole('menuitem', { name: 'Open in new tab', exact: true })).toBeVisible();
      await page.getByRole('menuitem', { name: 'Copy answer', exact: true }).click();
      expect(await copied(page)).toBe(ANSWER);
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
      const attaches = sameSessionAttaches(engine);
      await folded.click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Open in new tab', exact: true }).click();
      await expect(page.getByRole('tab')).toHaveCount(count + 1);
      await expect(page.getByRole('tab', { selected: true })).toHaveAttribute('id', active!);
      // A background tab does not attach until it is shown. Showing it posts the same session file.
      await page.getByRole('tab', { selected: false }).last().click();
      await expect.poll(() => sameSessionAttaches(engine)).toBe(attaches + 1);
      await expect(page.locator('.conversation-scroll:visible').getByText('Here is the patch.', { exact: true })).toBeVisible();
      await expect(page.locator('.conversation-scroll:visible').getByText('Clamp line 1 stays whole.')).toBeVisible();
    });

    test('a plain send stays at 60% and hides Copy until the engine records it', async ({ page }) => {
      let release: () => void = () => {};
      const held = new Promise<void>(resolve => { release = resolve; });
      await installMockEngine(page, {
        initial: { entries: [], running: false },
        turns: [{ entries: [{ Role: 'assistant', Text: 'Noted.', Answer: true }] }],
      });
      await page.route('**/api/engine/sessions/*/turn', async route => {
        await held;
        await route.fallback();
      });
      try {
        await openApp(page);
        await message(page).fill('And update the changelog.');
        await page.getByRole('button', { name: 'Send', exact: true }).click();
        const pending = page.locator('.user-message[data-sending]');
        await expect(pending).toBeVisible();
        await expect(pending).toHaveCSS('opacity', '0.6');
        await expect(pending.getByRole('button', { name: 'Copy message' })).toHaveCount(0);
        await page.setViewportSize({ width: 320, height: 800 });
        await expect(pending).toHaveCSS('opacity', '0.6');
        await expectNoHorizontalOverflow(page);
        release();
        await expect(pending).toHaveCount(0);
        await expect(page.locator('.user-message')).toHaveCSS('opacity', '1');
        await expect(page.locator('.user-message-text')).toHaveText('And update the changelog.');
        await expect(page.getByRole('button', { name: 'Copy message', exact: true })).toBeAttached();
      } finally {
        release();
      }
    });
  });
}

test.describe('conversation messages · touch', () => {
  test.use({ hasTouch: true });

  for (const theme of ['light', 'dark'] as const) {
    test(`${theme}: Copy stays visible without a hover`, async ({ page }) => {
      await page.addInitScript(themeName => { localStorage.setItem('codeaf-theme', themeName); }, theme);
      await page.emulateMedia({ colorScheme: theme });
      await installMockEngine(page, {
        initial: { entries: [], running: false },
        turns: [{ entries: [{ Role: 'assistant', Text: 'Noted.', Answer: true }] }],
      });
      await openApp(page);
      await message(page).fill('Keep the copy control');
      await page.getByRole('button', { name: 'Send', exact: true }).click();
      await expect(page.getByText('Noted.', { exact: true })).toBeVisible();
      expect(await page.evaluate(() => matchMedia('(hover: none)').matches || matchMedia('(pointer: coarse)').matches)).toBe(true);
      await page.mouse.move(0, 0);
      const copy = page.getByRole('button', { name: 'Copy message', exact: true });
      await expect(page.locator('.user-message-actions')).toHaveCSS('opacity', '1');
      await expect(copy).toBeVisible();
      await expect(copy).toHaveAttribute('data-size', 'message');
      // A coarse pointer enlarges the control to the shared hit target. The 26px message size is the fine-pointer size.
      const width = await copy.evaluate(el => parseFloat(getComputedStyle(el).width));
      expect(width).toBeGreaterThanOrEqual(26);
    });
  }
});

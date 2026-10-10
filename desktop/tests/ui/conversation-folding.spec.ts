import { test, expect, type Locator, type Page } from '@playwright/test';
import type { EngineEntry } from '../../src/features/chat/engine-client';
import design from '../../src/design/tokens.json' with { type: 'json' };
import { installMockEngine, type Scenario } from './support/mock-engine';
import { openApp } from './support/conversation';
import { expectAccessible, tokenColor } from './contracts';

// CV-031, CV-033, CV-034, CV-035. Design conversation 1e (long history) and 1f
// (Folding, Jump): beyond twelve turns the oldest sit in one group, the summary
// divider is two hairlines around the words, expanding a folded turn holds its
// top edge, and the platform chord steps between user messages.

const LIMIT = 12;
const OFFSET = 72;
const SESSION = 'mock-session-1.jsonl';
const WIDTHS = [320, 600, 1200] as const;
const THEMES = ['light', 'dark'] as const;

const question = (index: number) => `Question ${index} about the folding row`;
const digest = (index: number) => `Answer ${index} is the digest line.`;

test('the fold figures stay the ones the long-history card draws', () => {
  expect(design.turnFold.limit).toBe(LIMIT);
  expect(design.turnFold.jumpOffset).toBe(OFFSET);
});

function history(count: number, options?: { note?: boolean; longLast?: boolean }): Scenario {
  const entries: EngineEntry[] = [];
  for (let index = 0; index < count; index++) {
    entries.push({ Role: 'user', Text: question(index) });
    // A note inside the oldest turn is what makes the earlier lines a summary. A long history alone must not.
    if (options?.note && index === 0) entries.push({ Role: 'note', Text: 'Earlier messages summarized' });
    const last = index === count - 1;
    const text = options?.longLast && last
      ? Array.from({ length: 36 }, (_, paragraph) => `Paragraph ${paragraph + 1} of the latest answer fills the reading column.`).join('\n\n')
      : digest(index);
    entries.push({ Role: 'assistant', Text: text, Answer: true });
  }
  return { initial: { title: 'Folding', sessionFile: SESSION, entries, running: false } };
}

/** A saved chat reattaches on open. A fresh tab stays quiet until the first send, so the history has to already be the tab's session. */
async function seedChat(page: Page) {
  const state = {
    tabs: [{ id: 'folding', title: 'Folding', titleSource: 'manual', kind: 'conversation', pinned: false, draft: '', sessionFile: SESSION }],
    groups: [], closed: [], activeId: 'folding', nextNumber: 2, recentIds: ['folding'],
  };
  await page.addInitScript(value => {
    if (!localStorage.getItem('codeaf.desktop.workspace.v1')) localStorage.setItem('codeaf.desktop.workspace.v1', value);
  }, JSON.stringify(state));
}

function earlierName(count: number) {
  const older = count - LIMIT;
  return `${older} earlier ${older === 1 ? 'turn' : 'turns'}`;
}

const scroller = (page: Page) => page.locator('.conversation-scroll');
const turnOf = (page: Page, text: string) => page.locator('[data-turn]').filter({ hasText: text });

async function openHistory(page: Page, theme: (typeof THEMES)[number], scenario: Scenario, width = 1200) {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await seedChat(page);
  await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
  await page.setViewportSize({ width, height: 800 });
  await installMockEngine(page, scenario);
  await openApp(page);
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
}

async function paint(page: Page, token: string) {
  return page.evaluate(name => {
    const probe = document.createElement('span');
    probe.style.backgroundColor = `var(--${name})`;
    document.body.append(probe);
    const color = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return color;
  }, token);
}

async function cssVar(page: Page, name: string) {
  return page.evaluate(token => getComputedStyle(document.documentElement).getPropertyValue(token).trim(), name);
}

async function noOverflow(page: Page) {
  const overflow = await page.evaluate(() => {
    const pane = document.querySelector('.conversation-scroll');
    return document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1
      && !!pane && pane.scrollWidth <= pane.clientWidth + 1;
  });
  expect(overflow).toBe(true);
}

async function distanceToEnd(page: Page) {
  return scroller(page).evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop);
}

async function turnDelta(turn: Locator) {
  return turn.evaluate(el => {
    const pane = el.closest('.conversation-scroll')!;
    return el.getBoundingClientRect().top - pane.getBoundingClientRect().top;
  });
}

/** Puts a folded turn's top edge a fixed distance below the pane, and away from the end. */
async function park(page: Page, text: string, fromPaneTop: number) {
  await scroller(page).evaluate((pane, target) => {
    const row = [...pane.querySelectorAll<HTMLElement>('[data-turn]')].find(el => el.textContent?.includes(target.text));
    if (!row) throw new Error(`missing ${target.text}`);
    const delta = row.getBoundingClientRect().top - pane.getBoundingClientRect().top;
    pane.scrollTop += delta - target.fromPaneTop;
  }, { text, fromPaneTop });
}

async function modifier(page: Page) {
  return page.evaluate(() => (/Mac/.test(navigator.platform) ? 'Meta' : 'Control') as 'Meta' | 'Control');
}

async function blur(page: Page) {
  await page.evaluate(() => { (document.activeElement as HTMLElement | null)?.blur?.(); });
}

/** The user message whose turn top sits on the jump line, or null when none does. */
async function landed(page: Page) {
  return page.evaluate(offset => {
    const pane = document.querySelector('.conversation-scroll');
    if (!pane) return null;
    const paneTop = pane.getBoundingClientRect().top;
    for (const el of pane.querySelectorAll<HTMLElement>('[data-turn]')) {
      const delta = el.getBoundingClientRect().top - paneTop;
      if (Math.abs(delta - offset) <= 1) {
        const words = el.querySelector('.user-message-text, .turn-folded-user');
        return { text: words?.textContent ?? '', delta };
      }
    }
    return null;
  }, OFFSET);
}

for (const theme of THEMES) {
  test(`twelve turns stay in the flow (${theme})`, async ({ page }) => {
    await openHistory(page, theme, history(LIMIT));
    await expect(page.getByRole('button', { name: /earlier turn/ })).toHaveCount(0);
    await expect(page.getByRole('separator', { name: 'Earlier messages summarized' })).toHaveCount(0);
    for (let index = 0; index < LIMIT; index++) await expect(page.getByText(question(index), { exact: true })).toBeVisible();
  });

  test(`thirteen turns name the one older turn and draw no divider without a summary (${theme})`, async ({ page }) => {
    await openHistory(page, theme, history(LIMIT + 1));
    const older = page.getByRole('button', { name: earlierName(LIMIT + 1), exact: true });
    await expect(older).toBeVisible();
    await expect(older).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByText(question(0), { exact: true })).toHaveCount(0);
    await expect(page.getByText(question(1), { exact: true })).toBeVisible();
    await expect(page.getByRole('separator', { name: 'Earlier messages summarized' })).toHaveCount(0);
  });

  test(`earlier turns toggle and the summary divider sits between hairlines (${theme})`, async ({ page }) => {
    test.setTimeout(120_000);
    const count = LIMIT + 2;
    await openHistory(page, theme, history(count, { note: true }));
    const older = page.getByRole('button', { name: earlierName(count), exact: true });
    const divider = page.getByRole('separator', { name: 'Earlier messages summarized' });
    const gap = parseFloat(await cssVar(page, '--system-note-rule-gap'));
    const hairline = parseFloat(await cssVar(page, '--hairline'));
    const line = await paint(page, 'line');
    const ink3 = await tokenColor(page, 'ink-3');

    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      // Rest colour, not the ghost hover. The row is full width, so the pointer goes to the composer.
      await page.mouse.move(Math.min(width - 24, 280), 760);
      await expect(older).toBeVisible();
      await expect(older).toHaveAttribute('aria-expanded', 'false');
      await expect(older).toHaveCSS('height', await cssVar(page, '--row-height-v3'));
      await expect(older).toHaveCSS('color', ink3);
      await expect(older.locator('[data-icon="chevronRight"]')).toHaveCount(1);
      await expect.poll(() => older.locator('.earlier-chevron').evaluate(el => getComputedStyle(el).rotate)).toMatch(/^(none|0deg)$/);
      await expect(page.getByText(question(0), { exact: true })).toHaveCount(0);
      await expect(page.getByText(question(1), { exact: true })).toHaveCount(0);
      await expect(page.getByText(question(2), { exact: true })).toBeVisible();

      await expect(divider).toBeVisible();
      await expect(divider).toHaveCSS('color', ink3);
      await expect(divider).toHaveCSS('font-size', await cssVar(page, '--type-caption-size'));
      await expect(divider).toHaveCSS('font-weight', await cssVar(page, '--type-caption-weight'));
      await expect(divider).toHaveCSS('gap', await cssVar(page, '--system-note-rule-gap'));
      await expect(divider).toHaveCSS('margin-top', await cssVar(page, '--space-2'));
      await expect(divider).toHaveCSS('margin-bottom', await cssVar(page, '--space-2'));
      const rules = divider.locator('.summary-divider-rule');
      await expect(rules).toHaveCount(2);
      for (const rule of await rules.all()) {
        expect(parseFloat(await rule.evaluate(el => getComputedStyle(el).height))).toBeCloseTo(hairline, 2);
        await expect(rule).toHaveCSS('background-color', line);
      }
      const placed = await divider.evaluate(el => {
        const boxes = [...el.querySelectorAll('.summary-divider-rule')].map(rule => rule.getBoundingClientRect());
        const words = el.childNodes;
        const text = [...words].find(node => node.nodeType === Node.TEXT_NODE && node.textContent?.includes('Earlier'));
        const range = document.createRange();
        range.selectNodeContents(text!);
        const textBox = range.getBoundingClientRect();
        return {
          leftEnds: boxes[0].right,
          rightStarts: boxes[1].left,
          textStarts: textBox.left,
          textEnds: textBox.right,
          leftWidth: boxes[0].width,
          rightWidth: boxes[1].width,
        };
      });
      // The words sit between the two rules, and the rules share the row so the label stays centred.
      expect(placed.textStarts - placed.leftEnds).toBeGreaterThan(0);
      expect(placed.rightStarts - placed.textEnds).toBeGreaterThan(0);
      expect(Math.abs((placed.textStarts - placed.leftEnds) - gap)).toBeLessThanOrEqual(1);
      expect(Math.abs((placed.rightStarts - placed.textEnds) - gap)).toBeLessThanOrEqual(1);
      expect(Math.abs(placed.leftWidth - placed.rightWidth)).toBeLessThanOrEqual(1);
      expect(placed.leftWidth).toBeGreaterThan(8);
      await noOverflow(page);

      await older.click();
      await expect(older).toHaveAttribute('aria-expanded', 'true');
      await expect.poll(() => older.locator('.earlier-chevron').evaluate(el => getComputedStyle(el).rotate)).toBe('90deg');
      await expect(page.getByText(question(0), { exact: true })).toBeVisible();
      await expect(page.getByText(question(1), { exact: true })).toBeVisible();
      const order = await page.evaluate(() => {
        const pane = document.querySelector('.conversation-scroll')!;
        return [...pane.querySelectorAll('.earlier-row, .summary-divider, [data-turn]')].map(el => {
          if (el.classList.contains('earlier-row')) return 'earlier';
          if (el.classList.contains('summary-divider')) return 'divider';
          return el.querySelector('.turn-folded-user, .user-message-text')?.textContent ?? '';
        });
      });
      expect(order.slice(0, 4)).toEqual(['earlier', question(0), question(1), 'divider']);
      expect(order[4]).toBe(question(2));
      if (width === 320 || width === 1200) await expectAccessible(page);

      await older.click();
      await expect(older).toHaveAttribute('aria-expanded', 'false');
      await expect(page.getByText(question(0), { exact: true })).toHaveCount(0);
      await expect.poll(() => older.locator('.earlier-chevron').evaluate(el => getComputedStyle(el).rotate)).toMatch(/^(none|0deg)$/);
    }
  });

  test(`expanding a folded turn keeps its top line (${theme})`, async ({ page }) => {
    test.setTimeout(90_000);
    // Question 6 follows another folded turn, so opening it changes the margin above the row.
    // Without the scroll compensation that margin would drop the top line by the folded-turn gap.
    const held = question(6);
    await openHistory(page, theme, history(LIMIT + 2, { note: true, longLast: true }));
    await expect(turnOf(page, held)).toHaveAttribute('data-folded', 'true');

    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      await park(page, held, 180);
      const turn = turnOf(page, held);
      await expect.poll(() => distanceToEnd(page)).toBeGreaterThan(48);
      const before = await turnDelta(turn);
      expect(Math.abs(before - 180)).toBeLessThanOrEqual(1);
      const box = (await turn.boundingBox())!;
      expect(box.y).toBeGreaterThan(0);
      expect(box.y + box.height).toBeLessThan(800);

      await turn.getByRole('button', { name: 'Unfold', exact: true }).click();
      await expect(turn).not.toHaveAttribute('data-folded');
      await expect.poll(async () => Math.abs((await turnDelta(turn)) - before)).toBeLessThanOrEqual(1);
      await expect(turn.locator('.user-message-text')).toHaveText(held);

      // Folding uses the same hold, so the next width starts from a folded row again.
      await turn.hover();
      await turn.getByRole('button', { name: 'Fold', exact: true }).click();
      await expect(turn).toHaveAttribute('data-folded', 'true');
      await expect.poll(async () => Math.abs((await turnDelta(turn)) - before)).toBeLessThanOrEqual(1);
    }
  });

  test(`the platform chord steps between user messages and escape returns to the latest (${theme})`, async ({ page }) => {
    test.setTimeout(90_000);
    const count = LIMIT + 2;
    const latest = question(count - 1);
    const previous = question(count - 2);
    await openHistory(page, theme, history(count, { note: true, longLast: true }));
    const key = await modifier(page);
    const other = key === 'Meta' ? 'Control' : 'Meta';

    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 800 });
      await scroller(page).evaluate(el => { el.scrollTop = el.scrollHeight; });
      await expect.poll(() => distanceToEnd(page)).toBeLessThanOrEqual(1);
      // At the end of a long answer the latest turn's top is above the jump line, so the first step lands on it.
      expect(await turnDelta(turnOf(page, latest))).toBeLessThan(OFFSET - 1);

      await blur(page);
      await page.keyboard.press(`${key}+ArrowUp`);
      await expect.poll(() => landed(page)).toMatchObject({ text: latest });
      const first = (await landed(page))!;
      expect(Math.abs(first.delta - OFFSET)).toBeLessThanOrEqual(1);

      const beforeWrong = await turnDelta(turnOf(page, latest));
      await page.keyboard.press(`${other}+ArrowUp`);
      expect(Math.abs((await turnDelta(turnOf(page, latest))) - beforeWrong)).toBeLessThanOrEqual(1);

      await page.keyboard.press(`${key}+ArrowUp`);
      await expect.poll(() => landed(page)).toMatchObject({ text: previous });
      expect(Math.abs((await landed(page))!.delta - OFFSET)).toBeLessThanOrEqual(1);

      await page.keyboard.press(`${key}+ArrowDown`);
      await expect.poll(() => landed(page)).toMatchObject({ text: latest });
      expect(Math.abs((await landed(page))!.delta - OFFSET)).toBeLessThanOrEqual(1);

      await page.keyboard.press('Escape');
      await expect.poll(() => distanceToEnd(page)).toBeLessThanOrEqual(1);
      await expect(page.getByText('Paragraph 36 of the latest answer fills the reading column.')).toBeVisible();
    }
  });
}

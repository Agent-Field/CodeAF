import { test, expect, type Locator, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine, type MockEngine } from './support/mock-engine';
import { pendingQuestion } from './support/scenarios';
import {
  clarificationPair, clockedChoice, gitBatch, irreversibleChoice, pagerQuestions, singlePermission, tallPermission,
} from './support/scenarios-tray';
import { expectAccessible, tokenColor } from './contracts';
import { expectNoHorizontalOverflow, message, openApp, posts, send } from './support/conversation';

const tray = (page: Page) => page.getByRole('region', { name: 'Waiting on you' });
const opacity = (locator: Locator) => locator.evaluate((el) => Number(getComputedStyle(el).opacity));

/** Send once so the tab owns a session, then the engine asks; a reload reads the new state at once. */
async function openWithQuestions(page: Page, questions: EngineQuestion[], extra: Record<string, unknown> = {}): Promise<MockEngine> {
  const base = pendingQuestion();
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
  await openApp(page);
  await send(page, 'Set up storage');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({ needsPerson: true, running: true, questions, ...extra });
  await expect(async () => {
    await page.reload();
    await expect(tray(page)).toBeVisible({ timeout: 1500 });
  }).toPass({ timeout: 15_000 });
  return engine;
}

async function paint(page: Page, token: string, prop: 'color' | 'backgroundColor') {
  return page.evaluate(({ token, prop }) => {
    const probe = document.createElement('span');
    probe.style.color = `var(--${token})`;
    probe.style.backgroundColor = `var(--${token})`;
    document.body.append(probe);
    const value = getComputedStyle(probe)[prop];
    probe.remove();
    return value;
  }, { token, prop });
}

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.use({ colorScheme: scheme });

    test('CV-141 CV-142 CV-157: arrows page the focused tray, the end arrow is 40% , and the card rises in 380ms', async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'no-preference' });
      await openWithQuestions(page, pagerQuestions());
      const card = tray(page);
      const count = card.locator('.tray-count');
      const previous = card.getByRole('button', { name: 'Previous question' });
      const next = card.getByRole('button', { name: 'Next question' });
      const motion = await card.evaluate((el) => {
        const style = getComputedStyle(el);
        return { name: style.animationName, duration: style.animationDuration };
      });
      expect(motion.name).toContain('surface-enter');
      expect(motion.duration === '0.38s' || motion.duration === '380ms').toBe(true);

      await expect(count).toHaveAccessibleName('Question 1 of 3');
      await expect(previous).toBeDisabled();
      expect(await opacity(previous)).toBeCloseTo(0.4, 2);

      await message(page).focus();
      await page.keyboard.press('ArrowRight');
      await expect(count).toHaveAccessibleName('Question 1 of 3');

      await next.focus();
      await page.keyboard.press('ArrowRight');
      await expect(count).toHaveAccessibleName('Question 2 of 3');
      await page.keyboard.press('ArrowRight');
      await expect(count).toHaveAccessibleName('Question 3 of 3');
      await expect(card.getByRole('region', { name: 'Pick a region' })).toBeVisible();
      await expect(next).toBeDisabled();
      expect(await opacity(next)).toBeCloseTo(0.4, 2);
      await page.keyboard.press('ArrowRight');
      await expect(count).toHaveAccessibleName('Question 3 of 3');

      await previous.focus();
      await page.keyboard.press('ArrowLeft');
      await expect(count).toHaveAccessibleName('Question 2 of 3');
    });

    test('CV-143 CV-152: Allow once on Enter, Always allow…, Deny, no pager, and the holding-up line', async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const engine = await openWithQuestions(page, [singlePermission()]);
      const card = tray(page).getByRole('region', { name: 'Allow `rm -rf build`?' });
      const allow = card.getByRole('button', { name: 'Allow once', exact: true });
      const deny = card.getByRole('button', { name: 'Deny', exact: true });
      const always = card.getByRole('button', { name: 'Always allow…' });
      await expect(allow).toHaveClass(/button-primary/);
      await expect(deny).toHaveClass(/button-quiet/);
      await expect(always).toHaveClass(/button-ghost/);
      await expect(allow).toHaveCSS('background-color', await paint(page, 'accent', 'backgroundColor'));
      await expect(deny).toHaveCSS('background-color', await paint(page, 'field', 'backgroundColor'));
      await expect(always).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(tray(page).getByRole('button', { name: 'Next question' })).toHaveCount(0);
      const holding = card.locator('.tray-holding');
      await expect(holding).toHaveText('Holding up 2 tasks: Update fixtures, Port fix to v1');
      await expect(holding).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(holding).toHaveCSS('font-size', '12px');
      await expectAccessible(page);

      await always.focus();
      await page.keyboard.press('Enter');
      await expect(card.getByRole('button', { name: 'For this task' })).toBeVisible();
      await expect(card.getByRole('button', { name: 'Everywhere, from now on' })).toBeVisible();
      await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur(); });
      await page.keyboard.press('Enter');
      await expect.poll(() => posts(engine, '/answer').length).toBe(1);
      expect(posts(engine, '/answer')[0].body).toMatchObject({ kind: 'consent', id: 1, key: '1', picked: ['1'] });
    });

    test('CV-146 CV-147: typing in the card holds the clock, and the held line is On hold — take your time', async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const engine = await openWithQuestions(page, [clockedChoice()]);
      const card = tray(page);
      const clock = card.getByRole('timer');
      await expect(clock).toHaveText(/^Picks Suggested in \d+s$/);
      expect(posts(engine, '/questions/hold')).toHaveLength(0);
      const note = card.getByRole('textbox', { name: 'Add a note' });
      await note.focus();
      await page.keyboard.type('because');
      await expect(clock).toHaveText('On hold — take your time');
      await expect.poll(() => posts(engine, '/questions/hold').length).toBe(1);
      expect(posts(engine, '/questions/hold')[0].body).toMatchObject({ kind: 'ask', id: 14 });
      await expect(clock).toHaveCSS('color', await tokenColor(page, 'ink-2'));
    });

    test('CV-148: irreversible Keep it, Remove, Tell it… — no clock, no Always, no reason field', async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const engine = await openWithQuestions(page, [irreversibleChoice()]);
      const card = tray(page).getByRole('region', { name: 'Remove the legacy YAML loader?' });
      const buttons = card.locator('.answer-row .answer');
      await expect(buttons).toHaveText(['Keep it', 'Remove', 'Tell it…']);
      const keep = card.getByRole('button', { name: 'Keep it', exact: true });
      const remove = card.getByRole('button', { name: 'Remove', exact: true });
      const tell = card.getByRole('button', { name: 'Tell it…', exact: true });
      await expect(keep).toHaveClass(/button-quiet/);
      await expect(remove).toHaveClass(/button-danger/);
      await expect(tell).toHaveClass(/button-ghost/);
      await expect(keep).toHaveCSS('background-color', await paint(page, 'field', 'backgroundColor'));
      await expect(remove).toHaveCSS('background-color', await paint(page, 'danger-soft', 'backgroundColor'));
      await expect(remove).toHaveCSS('color', await paint(page, 'danger', 'color'));
      await expect(tell).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(card.getByRole('button', { name: /Always/ })).toHaveCount(0);
      await expect(card.getByRole('timer')).toHaveCount(0);
      await expect(card.getByRole('button', { name: 'Hold' })).toHaveCount(0);
      await expect(card.getByRole('textbox', { name: 'Say why (optional)' })).toHaveCount(0);
      await expect(card.getByRole('button', { name: 'and say why' })).toHaveCount(0);
      await expect(card.getByRole('button', { name: 'You decide' })).toHaveCount(0);
      await tell.focus();
      await page.keyboard.press('Enter');
      expect(posts(engine, '/answer')).toHaveLength(0);
      await expect(card.getByRole('textbox', { name: 'What should it know?' })).toBeVisible();
      await expectAccessible(page);
    });

    test('CV-149: clarification takes focus, ⌘↵ sends, Later folds into the count, You decide answers for the asker', async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const engine = await openWithQuestions(page, clarificationPair());
      const card = tray(page);
      const field = card.getByRole('textbox', { name: 'Your answer' });
      await expect(field).toBeFocused();
      const later = card.getByRole('button', { name: 'Later', exact: true });
      const decide = card.getByRole('button', { name: 'You decide', exact: true });
      const ink = await tokenColor(page, 'ink-2');
      await expect(later).toHaveCSS('color', ink);
      await expect(decide).toHaveCSS('color', ink);

      await later.focus();
      await page.keyboard.press('Enter');
      const folded = card.getByRole('button', { name: '1 later' });
      await expect(folded).toBeVisible();
      await expect(card.getByRole('region', { name: 'Which customers still run v1?' })).toHaveCount(0);
      await folded.focus();
      await page.keyboard.press('Enter');
      await card.getByRole('button', { name: 'Answer now' }).focus();
      await page.keyboard.press('Enter');
      await expect(field).toBeFocused();

      await field.fill('Northwind and Acme');
      await field.press('Meta+Enter');
      await expect.poll(() => posts(engine, '/answer').length).toBe(1);
      expect(posts(engine, '/answer')[0].body).toMatchObject({ kind: 'ask', id: 8, change: 'Northwind and Acme' });

      const second = card.getByRole('textbox', { name: 'Your answer' });
      await expect(second).toBeFocused();
      await card.getByRole('button', { name: 'You decide', exact: true }).focus();
      await page.keyboard.press('Enter');
      await expect.poll(() => posts(engine, '/answer').length).toBe(2);
      expect(posts(engine, '/answer')[1].body).toMatchObject({ kind: 'ask', id: 9, key: 'east', decidedBy: 'asker' });
    });

    test('CV-150: One by one becomes an in-card pager, and Deny all denies every command', async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const engine = await openWithQuestions(page, gitBatch());
      const card = tray(page).getByRole('region', { name: 'Allow 3 actions?' });
      await expect(card.getByRole('button', { name: 'Allow all' })).toHaveClass(/button-primary/);
      await expect(card.getByRole('button', { name: 'Deny all' })).toHaveClass(/button-quiet/);
      await expect(card.getByText('git checkout release/v1')).toBeVisible();
      await expect(card.getByText('git cherry-pick 3f2a1c')).toBeVisible();
      await expect(card.getByText('git push origin release/v1')).toBeVisible();
      await expect(tray(page).getByRole('button', { name: 'Next question' })).toHaveCount(0);

      const one = card.getByRole('button', { name: 'One by one' });
      await one.focus();
      await page.keyboard.press('Enter');
      await expect(card.getByText('1 of 3')).toBeVisible();
      await expect(card.getByRole('heading', { name: 'git checkout release/v1' })).toBeVisible();
      const next = card.getByRole('button', { name: 'Next action' });
      await next.focus();
      await page.keyboard.press('Enter');
      await page.keyboard.press('Enter');
      await expect(card.getByText('3 of 3')).toBeVisible();
      await expect(next).toBeDisabled();
      expect(await opacity(next)).toBeCloseTo(0.4, 2);
      await card.getByRole('button', { name: 'Previous action' }).focus();
      await page.keyboard.press('Enter');
      await expect(card.getByText('2 of 3')).toBeVisible();

      await page.reload();
      await expect(tray(page).getByRole('region', { name: 'Allow 3 actions?' })).toBeVisible();
      const again = tray(page).getByRole('region', { name: 'Allow 3 actions?' });
      await again.getByRole('button', { name: 'Deny all' }).focus();
      await page.keyboard.press('Enter');
      await expect.poll(() => posts(engine, '/answer').length).toBe(3);
      expect(posts(engine, '/answer').map((call) => call.body.key)).toEqual(['3', '3', '3']);
    });

    test('CV-155 CV-156: receipts, including picked by codeaf, and a receipt brings its question forward', async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      const at = new Date(2026, 9, 9, 14, 2).toISOString();
      const recentOutcomes = [
        { kind: 'ask', token: '4', outcome: 'decided', words: 'Keep strict', by: 'dial', elapsedSeconds: 30, at, callId: 'ask-clock' },
        { kind: 'ask', token: '5', outcome: 'decided', words: 'Use Postgres', by: 'person', at, callId: 'ask-person' },
        { kind: 'ask', token: '6', outcome: 'withdrawn', words: '', callId: 'ask-gone' },
      ];
      const waiting = pagerQuestions().slice(0, 2);
      const base = pendingQuestion();
      const engine = await installMockEngine(page, {
        ...base,
        initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] },
        turns: [{
          entries: ['ask-clock', 'ask-person', 'ask-gone'].map((CallID) => ({
            Role: 'tool' as const, Tool: 'ask', CallID, Text: '', Answered: true, Output: 'Answered',
          })),
        }],
      });
      await openApp(page);
      await send(page, 'Choose the parser policy');
      await expect.poll(() => posts(engine, '/turn').length).toBe(1);
      engine.update({ needsPerson: true, running: true, questions: waiting, recentOutcomes } as never);
      await expect(async () => {
        await page.reload();
        await expect(tray(page)).toBeVisible({ timeout: 1500 });
      }).toPass({ timeout: 15_000 });

      const auto = page.locator('.receipt[data-state="answered"]').filter({ hasText: 'Keep strict' });
      await expect(auto.locator('.receipt-rest')).toHaveText('picked by codeaf after 30s');
      await expect(auto.locator('.app-icon[data-icon="check"]')).toHaveCount(1);
      const person = page.locator('.receipt[data-state="answered"]').filter({ hasText: 'Use Postgres' });
      await expect(person.locator('.receipt-rest')).toHaveText('· you · 14:02');
      await expect(person.locator('.receipt-label')).toHaveCSS('color', await tokenColor(page, 'ink-2'));
      await expect(person.locator('.receipt-rest')).toHaveCSS('color', await tokenColor(page, 'ink-3'));
      await expect(page.locator('.receipt[data-state="withdrawn"]')).toContainText('No longer needed. The turn moved on.');
      await expect(page.locator('.receipt[data-state="waiting"]')).toHaveCount(2);

      await expect(tray(page).getByRole('region', { name: 'Pick a database' })).toBeVisible();
      const second = page.getByRole('button', { name: 'Waiting on you: Name the cache' });
      await second.focus();
      await page.keyboard.press('Enter');
      await expect(tray(page).getByRole('region', { name: 'Name the cache' })).toBeVisible();
      await page.getByRole('button', { name: 'Waiting on you: Pick a database' }).click();
      await expect(tray(page).getByRole('region', { name: 'Pick a database' })).toBeVisible();
      await expect(tray(page).locator('.tray-count')).toHaveAccessibleName('Question 1 of 2');
    });

    test('CV-153: the body stays within 40% at 320, 600 and 1200, with a 16px mask and pinned actions', async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
      await openWithQuestions(page, [tallPermission()]);
      for (const width of [320, 600, 1200]) {
        await page.setViewportSize({ width, height: 640 });
        const card = tray(page);
        const body = card.locator('.tray-body');
        const scroll = card.locator('.tray-scroll');
        const allow = card.getByRole('button', { name: 'Allow once', exact: true });
        await expect(scroll).toHaveAttribute('class', /tray-scroll/);
        await scroll.evaluate((el) => { el.scrollTop = 0; });
        await expect(body).toHaveAttribute('data-more', 'true');
        const measured = await body.evaluate((el) => {
          const fade = getComputedStyle(document.documentElement).getPropertyValue('--tray-fade').trim();
          const scrollEl = el.querySelector('.tray-scroll')!;
          const mask = getComputedStyle(scrollEl).maskImage || getComputedStyle(scrollEl).webkitMaskImage;
          return {
            fade,
            max: parseFloat(getComputedStyle(el).maxHeight),
            height: el.getBoundingClientRect().height,
            mask,
          };
        });
        expect(measured.fade).toBe('16px');
        expect(measured.max).toBeCloseTo(640 * 0.4, 0);
        expect(measured.height).toBeLessThanOrEqual(measured.max + 1);
        expect(measured.mask).toContain('linear-gradient');
        const before = await allow.boundingBox();
        await scroll.evaluate((el) => {
          el.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
          el.scrollTop = el.scrollHeight;
        });
        const after = await allow.boundingBox();
        const moved = await scroll.evaluate((el) => el.scrollTop);
        expect(moved).toBeGreaterThan(20);
        expect(Math.abs(before!.y - after!.y)).toBeLessThan(1);
        await expect(allow).toBeInViewport();
        await expectNoHorizontalOverflow(page);
      }
    });
  });
}

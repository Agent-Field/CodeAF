import { openPage } from './support/shell-navigation';
import { test, expect, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion, plainReply, withTasks } from './support/scenarios';
import { richReply, trayQuestions } from './support/scenarios-v2';
import { expectAccessible, tokenColor } from './contracts';
import { openApp, posts, send } from './support/conversation';

// ink-3 is the designer exact colour for muted text and reads 3.1 to 4.5:1 on the surfaces. The owner rule is that the design wins,
// so only the color-contrast rule is waived, on the INK3_TEXT selectors in contracts.ts; every other axe rule applies to everything. This sweep opens each conversation surface in both schemes and lets axe judge
// every visible text node, so ink-3 text outside those selectors fails here rather than in review.

const tray = (page: Page) => page.getByRole('region', { name: 'Waiting on you' });

async function openWithQuestions(page: Page, questions: EngineQuestion[]) {
  const base = pendingQuestion();
  const engine = await installMockEngine(page, { ...base, initial: { ...base.initial, entries: [], needsPerson: false, running: false, questions: [] } });
  await openApp(page);
  await send(page, 'Set up storage');
  await expect.poll(() => posts(engine, '/turn').length).toBe(1);
  engine.update({ needsPerson: true, running: true, questions });
  await expect(async () => {
    await page.reload();
    await expect(tray(page)).toBeVisible({ timeout: 1500 });
  }).toPass({ timeout: 15_000 });
}

for (const scheme of ['light', 'dark'] as const) {
  test.describe(scheme, () => {
    test.beforeEach(async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
    });

    test('Design system specimen', async ({ page }) => {
      await page.goto('/');
      await openPage(page, 'Design system');
      await expect(page.locator('.page-title')).toHaveText('Design system');
      const warning = page.locator('.closing-specimen .toast').last();
      await warning.evaluate(toast => toast.setAttribute('data-tone', 'warning'));
      await expect(warning.locator('.toast-text')).toHaveCSS('color', await tokenColor(page, 'ink'));
      for (const scroll of await page.locator('.tray-scroll, .tray-compare').all()) await expect(scroll).toHaveAttribute('tabindex', '0');
      await expectAccessible(page);
    });

    test('a quiet composer and a plain reply', async ({ page }) => {
      await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
      await openApp(page);
      await expectAccessible(page);
      await send(page, 'Explain the add helper');
      await expect(page.getByRole('table')).toBeVisible();
      await expectAccessible(page);
    });

    test('work opened to steps, an edit diff and a terminal', async ({ page }) => {
      await installMockEngine(page, richReply());
      await openApp(page);
      await send(page, 'Fix the flaky login test and draw a logo');
      await expect(page.getByText('so the login test no longer reads the real time.')).toBeVisible();
      await expect(page.locator('.link-chip-title')).toHaveCSS('color', await tokenColor(page, 'ink'));
      await expectAccessible(page);
      await page.getByRole('button', { name: /^Worked \d+s · / }).click();
      await page.getByRole('button', { name: /^Pinning the clock/ }).click();
      await page.getByRole('button', { name: 'edit internal/auth/auth_test.go' }).click();
      await expectAccessible(page);
    });

    test('tasks panel, a task notice and its task view', async ({ page }) => {
      const base = withTasks();
      const aside = { Role: 'aside', Text: 'Migrate the settings screen done · ran 30s · Two parts finished.', TaskIDs: ['2'] };
      await installMockEngine(page, {
        ...base,
        initial: { title: base.initial.title, entries: [] },
        turns: [{ entries: [aside, base.initial.entries!.at(-1)!] as typeof base.initial.entries, patch: { tasks: base.initial.tasks } }],
      });
      await openApp(page);
      await send(page, 'Migrate the settings screen');
      await expect(page.getByRole('complementary', { name: 'Tasks' })).toBeVisible();
      await expectAccessible(page);
      await page.locator('.turn-v2').getByRole('button', { name: /Migrate the settings screen/ }).click();
      await expect(page.getByRole('navigation', { name: 'Breadcrumb' })).toBeVisible();
      await page.getByRole('button', { name: 'Instructions' }).click();
      await expectAccessible(page);
    });

    test('the decision tray', async ({ page }) => {
      await openWithQuestions(page, trayQuestions());
      await expectAccessible(page);
    });

    test('the muted-text waiver stays limited to design roles and contrast', async ({ page }) => {
      await page.goto('/');
      const ink = await tokenColor(page, 'ink-3');
      const canvas = await tokenColor(page, 'canvas');
      // setContent writes with document.open and leaves this page's timers running, so the
      // disconnected-engine toast can land in the fixture. Unload the app first.
      await page.goto('about:blank');
      await page.setContent(`<html lang="en"><head><title>Contrast contract</title></head><body style="background:${canvas};color:${ink}"><main>
        <kbd class="keyboard-shortcut">Ctrl W</kbd>
        <div class="rail-row" data-busy-closed><span class="nav-label">Closed and still running</span></div>
      </main></body></html>`);
      await expectAccessible(page);
      // An ordinary rail title cannot inherit the closed-row exception.
      await page.locator('.rail-row').evaluate(row => row.removeAttribute('data-busy-closed'));
      await expect(expectAccessible(page)).rejects.toThrow('color-contrast');
      await page.locator('.rail-row').evaluate(row => row.setAttribute('data-busy-closed', ''));
      // Matching muted text still has to satisfy every other accessibility rule.
      await page.locator('main').evaluate(root => root.insertAdjacentHTML('beforeend', '<button class="keyboard-shortcut"></button>'));
      await expect(expectAccessible(page)).rejects.toThrow('button-name');
    });
  });
}

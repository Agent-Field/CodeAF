import { test, expect, type Page } from '@playwright/test';
import type { EngineQuestion } from '../../src/features/chat/engine-client';
import { installMockEngine } from './support/mock-engine';
import { pendingQuestion, plainReply, withTasks } from './support/scenarios';
import { richReply, trayQuestions } from './support/scenarios-v2';
import { expectAccessible } from './contracts';
import { openApp, posts, send } from './support/conversation';

// ink-3 is the designer's exact colour and reads 3.1 to 4.5:1 on the surfaces, so TEXT never uses it
// (docs/COMPONENTS.md). This sweep opens each conversation surface in both schemes and lets axe judge
// every visible text node, so a new ink-3 text rule fails here rather than in review.

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
      await page.getByRole('button', { name: 'Design system', exact: true }).click();
      await expect(page.locator('.page-title')).toHaveText('Design system');
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
      await expectAccessible(page);
      await page.getByRole('button', { name: /^Worked \d+s/ }).click();
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
  });
}

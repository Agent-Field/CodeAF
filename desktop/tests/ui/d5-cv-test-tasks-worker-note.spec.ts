import { test, expect } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { withTasks } from './support/scenarios';
import { openApp, send } from './support/conversation';

for (const theme of ['light', 'dark'] as const) {
  test(`CV-125 worker notes use the design voice and geometry · ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    const scenario = withTasks();
    const body = 'The nested fixture had a second bug, a missing closing brace. I fixed it too.';
    scenario.taskPages!['2'].Notes = [
      { Author: 'worker-1', Body: body },
      { Author: '2.1', Body: 'A second recorded worker note.' },
      { Person: true, Body: 'Also cover the empty-array case.' },
      { Author: 'chat', Body: 'Keep strict-mode fixtures unchanged.' },
      { Author: 'worker-1', Body: '   ' },
    ];
    await installMockEngine(page, scenario);
    await openApp(page);
    await send(page, 'Migrate the settings screen');
    await page.getByRole('complementary', { name: 'Tasks' })
      .getByRole('button', { name: /^(?!Collapse|Expand).*Migrate the settings screen/ }).click();
    await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
    const notes = page.getByRole('list', { name: 'Notes' });
    await expect(notes.getByText('Note from worker', { exact: true })).toHaveCount(2);
    await expect(notes.getByText('Conversation', { exact: true })).toBeVisible();
    await expect(notes.locator('.task-note-person .task-note-bubble')).toHaveText('Also cover the empty-array case.');
    await expect(notes).not.toContainText('worker-1');
    const worker = notes.locator('.task-note-other').filter({ hasText: body });
    const metrics = await worker.evaluate(el => {
      const label = getComputedStyle(el.querySelector('.task-note-author')!);
      const body = getComputedStyle(el.querySelector('.task-note-body')!);
      const style = getComputedStyle(el);
      const rect = el.getBoundingClientRect();
      const parent = el.parentElement!.getBoundingClientRect();
      return {
        labelSize: label.fontSize, labelWeight: label.fontWeight, labelLeading: label.lineHeight,
        bodySize: body.fontSize, bodyLeading: body.lineHeight,
        gap: style.gap, maxWidth: style.maxWidth, background: style.backgroundColor,
        left: rect.left - parent.left, widthRatio: rect.width / parent.width,
      };
    });
    expect(metrics.labelSize).toBe('11px');
    expect(metrics.labelWeight).toBe('500');
    expect(metrics.labelLeading).toBe('normal');
    expect(metrics.bodySize).toBe('13px');
    expect(Number.parseFloat(metrics.bodyLeading)).toBeCloseTo(20.8, 4);
    expect(metrics.gap).toBe('2px');
    expect(metrics.maxWidth).toBe('85%');
    expect(metrics.left).toBeCloseTo(0);
    expect(metrics.widthRatio).toBeLessThanOrEqual(0.851);
    // Resolve tokens through the browser so equivalent colour serializations compare equally.
    for (const [selector, token] of [['.task-note-author', '--ink-3'], ['.task-note-body', '--ink-2']]) {
      expect(await worker.locator(selector).evaluate((el, token) => {
        const probe = document.createElement('span');
        probe.style.color = `var(${token})`;
        el.append(probe);
        const matches = getComputedStyle(el).color === getComputedStyle(probe).color;
        probe.remove();
        return matches;
      }, token)).toBe(true);
    }
    expect(metrics.background).toBe('rgba(0, 0, 0, 0)');
    for (const width of [320, 480, 600, 800, 1200]) {
      await page.setViewportSize({ width, height: 560 });
      await expect.poll(() => worker.evaluate(el => {
        const rect = el.getBoundingClientRect();
        const parent = el.parentElement!.getBoundingClientRect();
        return rect.width <= parent.width * 0.85 + 1 && Math.abs(rect.left - parent.left) < 1
          && el.scrollWidth <= el.clientWidth + 1;
      }), { message: `Worker note fits at ${width}px` }).toBe(true);
    }
  });

  test(`CV-125 absent task-page notes draw nothing · ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    const scenario = withTasks();
    delete scenario.taskPages!['2'].Notes;
    await installMockEngine(page, scenario);
    await openApp(page);
    await send(page, 'Migrate the settings screen');
    await page.getByRole('complementary', { name: 'Tasks' })
      .getByRole('button', { name: /^(?!Collapse|Expand).*Migrate the settings screen/ }).click();
    await expect(page.getByRole('heading', { name: 'Migrate the settings screen' })).toBeVisible();
    await expect(page.getByRole('list', { name: 'Notes' })).toHaveCount(0);
    await expect(page.getByText('Note from worker', { exact: true })).toHaveCount(0);
  });
}

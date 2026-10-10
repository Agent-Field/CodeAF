import { test, expect, type Page } from '@playwright/test';
import { expectAccessible } from './contracts';
import { expectNoHorizontalOverflow } from './support/conversation';
import { installMockEngine } from './support/mock-engine';

// SH-240/241 retire the palette in favour of Shell 3f/3k's New-tab field.
// Platform emulation exercises both primary modifiers even on a Linux browser runner.
const platforms = [
  { name: 'Mac', platform: 'MacIntel', modifier: 'Meta' },
  { name: 'Linux', platform: 'Linux x86_64', modifier: 'Control' },
] as const;
const field = (page: Page) => page.getByRole('combobox', { name: 'Search or start', exact: true });

async function expectNoLegacyPalette(page: Page) {
  await expect(page.getByRole('dialog', { name: /command|search/i })).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Search commands' })).toHaveCount(0);
  // Hidden rail markup and accessible names count too, so a closed narrow drawer cannot hide a regression.
  expect(await page.locator('body').evaluate(body => {
    const labels = [...body.querySelectorAll('[aria-label], [placeholder], [title]')]
      .flatMap(element => ['aria-label', 'placeholder', 'title'].map(name => element.getAttribute(name) ?? ''));
    return /Find anything|Go to Activity/i.test([body.textContent, ...labels].join('\n'));
  })).toBe(false);
}

for (const theme of ['light', 'dark'] as const) {
  for (const width of [320, 600, 1200]) {
    for (const platform of platforms) {
      test(`SH-240/241 ${platform.name} K focuses and reuses New tab · ${theme} · ${width}px`, async ({ page }) => {
        await page.setViewportSize({ width, height: 800 });
        await page.emulateMedia({ colorScheme: theme });
        await page.addInitScript(({ theme, platform }) => {
          Object.defineProperty(navigator, 'platform', { value: platform });
          localStorage.setItem('codeaf-theme', theme);
          localStorage.setItem('codeaf.desktop.workspace.v1', JSON.stringify({
            tabs: [{ id: 'reading', kind: 'conversation', title: 'Reading', titleSource: 'manual',
              draft: '', pinned: false, sessionFile: '/workspace/palette/session.jsonl' }],
            groups: [], closed: [], activeId: 'reading', nextNumber: 2, recentIds: ['reading'],
          }));
        }, { theme, platform: platform.platform });
        const engine = await installMockEngine(page, { initial: {
          sessionFile: '/workspace/palette/session.jsonl',
          entries: [{ Role: 'user', Text: 'Keep this conversation open.' }],
        } });
        await page.goto('/');
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await expect(page.getByText('Keep this conversation open.', { exact: true })).toBeVisible();
        await expect(page.getByRole('tab')).toHaveCount(1);
        await expectNoLegacyPalette(page);

        // The chord starts in the actual conversation composer, rather than a specimen or the empty shell.
        await page.getByRole('textbox', { name: 'Message', exact: true }).focus();
        await page.keyboard.press(`${platform.modifier}+k`);
        const newTab = page.getByRole('tab', { name: 'New tab', exact: true });
        await expect(newTab).toHaveAttribute('aria-selected', 'true');
        await expect(field(page)).toBeFocused();
        await expect(field(page)).toHaveValue('');
        await expect(field(page)).toBeInViewport();
        await expect(page.getByRole('tab')).toHaveCount(2);
        await expect(page.getByRole('dialog')).toHaveCount(0);
        const identity = await newTab.getAttribute('id');
        expect(identity).toBeTruthy();
        await expectNoLegacyPalette(page);
        await expectNoHorizontalOverflow(page);
        await expectAccessible(page);

        // Move focus away first: a no-op shortcut must not satisfy the second-focus contract.
        await field(page).press('Tab');
        await expect(field(page)).not.toBeFocused();
        await page.keyboard.press(`${platform.modifier}+k`);
        await expect(field(page)).toBeFocused();
        await expect(newTab).toHaveAttribute('id', identity!);
        await expect(page.getByRole('tab')).toHaveCount(2);

        // Return using the strip's keyboard activation, then prove the same empty tab is selected again.
        const reading = page.getByRole('tab', { name: 'Reading', exact: true });
        await reading.focus();
        await reading.press('Enter');
        await expect(reading).toHaveAttribute('aria-selected', 'true');
        await page.getByRole('textbox', { name: 'Message', exact: true }).focus();
        await page.keyboard.press(`${platform.modifier}+k`);
        await expect(field(page)).toBeFocused();
        await expect(newTab).toHaveAttribute('aria-selected', 'true');
        await expect(newTab).toHaveAttribute('id', identity!);
        await expect(field(page)).toHaveValue('');
        await expect(page.getByRole('tab')).toHaveCount(2);
        await expectNoLegacyPalette(page);

        const showSidebar = page.getByRole('button', { name: 'Show sidebar', exact: true });
        if (await showSidebar.isVisible()) {
          await showSidebar.press('Enter');
          await expect(page.getByRole('dialog', { name: 'Navigation', exact: true })).toBeVisible();
          await expectNoLegacyPalette(page);
          await page.keyboard.press('Escape');
        }
        // Returning to Reading reattaches its saved session; the field must never create a new session or send work.
        expect(engine.calls.filter(call => call.method === 'POST' &&
          ((/\/sessions$/.test(call.path) && !call.body.sessionFile) || /\/(turn|stop)$/.test(call.path)))).toEqual([]);
      });
    }
  }
}

import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect, type Page } from '@playwright/test';
import { tokenColor } from './contracts';
import { openApp } from './support/conversation';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
import { openPage } from './support/shell-navigation';

/**
 * Read-only check of the Places hierarchy defaults and the Settings sections.
 * The fixture is the mock engine. Nothing here writes a model or a profile.
 */
const HIERARCHY = [
  { key: 'maxAiTopLevel', name: 'Top-level places codeaf may create', value: '6', min: '0', max: '50', provisional: 'Provisional default: 6 places' },
  { key: 'maxAiSiblings', name: 'Places codeaf may create under one parent', value: '8', min: '0', max: '100', provisional: 'Provisional default: 8 places' },
  { key: 'maxAiDepth', name: 'Deepest level for a new place', value: '3', min: '1', max: '6', provisional: 'Provisional default: 3 levels' },
  { key: 'maxAiPlaces', name: 'Places codeaf may create in all', value: '30', min: '0', max: '500', provisional: 'Provisional default: 30 places' },
] as const;

const SECTIONS = ['Pinned', 'Conversation and tasks', 'Naming and summaries', 'Places organization', 'Memory, routing and safety', 'Provider key', 'Appearance', 'Engine'];

async function chooseTheme(page: Page, theme: 'light' | 'dark') {
  await page.getByRole('combobox', { name: 'Theme', exact: true }).click();
  await page.getByRole('option', { name: `${theme === 'light' ? 'Light' : 'Dark'} appearance`, exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('data-resolved-theme', theme);
}

test('hierarchy defaults and settings sections, measured in light and dark', async ({ page }, testInfo) => {
  const shots = process.env.CODEAF_UI_RESULTS ?? testInfo.outputDir;
  mkdirSync(shots, { recursive: true });
  const engine = await installMockEngine(page, { ...plainReply(), initial: { entries: [], title: '' } });
  await openApp(page);

  for (const theme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' });
    await openPage(page, 'Settings');
    await chooseTheme(page, theme);
    await expect(page.getByRole('heading', { name: 'Models', level: 1 })).toBeVisible();
    await expect(page.getByRole('heading', { level: 2 })).toHaveText(SECTIONS);
    await expect(page.getByRole('heading', { name: 'Permissions', level: 2 })).toHaveCount(0);

    const places = page.getByRole('region', { name: 'Places organization', exact: true });
    const ink2 = await tokenColor(page, 'ink-2');
    const danger = await tokenColor(page, 'danger');
    expect(ink2).not.toBe(danger);

    for (const row of HIERARCHY) {
      const item = places.locator(`[data-setting="${row.key}"]`);
      const field = item.getByRole('spinbutton', { name: row.name, exact: true });
      await expect(field).toHaveValue(row.value);
      await expect(field).toHaveAttribute('min', row.min);
      await expect(field).toHaveAttribute('max', row.max);
      await expect(item).toContainText(row.provisional);
      const state = item.locator('.settings-row-state');
      const measured = await state.evaluate(node => {
        const style = getComputedStyle(node);
        const box = node.getBoundingClientRect();
        return { color: style.color, width: box.width, height: box.height };
      });
      expect(measured.color).toBe(ink2);
      expect(measured.color).not.toBe(danger);
      expect(measured.width).toBeGreaterThan(0);
      expect(measured.height).toBeGreaterThan(0);
    }

    const depth = places.locator('[data-setting="maxAiDepth"]');
    await expect(depth).toContainText('Places you make yourself can go deeper');
    const box = await places.evaluate(node => {
      const rect = node.getBoundingClientRect();
      return { width: rect.width, height: rect.height };
    });
    expect(box.width).toBeGreaterThan(200);
    expect(box.height).toBeGreaterThan(100);
    await places.screenshot({ path: join(shots, `settings-hierarchy-${theme}-${testInfo.project.name}.png`) });
  }

  const settingsWrites = engine.calls.filter(call =>
    (call.method === 'PUT' || call.method === 'POST') && (call.path.includes('/models/') || call.path.includes('/places/policy')));
  expect(settingsWrites).toEqual([]);
});

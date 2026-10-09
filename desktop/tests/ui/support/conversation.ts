import { expect, type Page } from '@playwright/test';
import type { MockEngine } from './mock-engine';

export const message = (page: Page) => page.getByRole('textbox', { name: 'Message', exact: true });

/** POSTs to one endpoint, e.g. posts(engine, '/turn'). */
export const posts = (engine: MockEngine, suffix: string) =>
  engine.calls.filter(c => c.method === 'POST' && c.path.endsWith(suffix));

export async function openApp(page: Page) {
  await page.goto('/');
  await expect(message(page)).toBeVisible();
}

export async function send(page: Page, text: string) {
  await message(page).fill(text);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
}

export async function expectNoHorizontalOverflow(page: Page) {
  expect(await page.evaluate(() => document.scrollingElement!.scrollWidth <= innerWidth)).toBe(true);
}

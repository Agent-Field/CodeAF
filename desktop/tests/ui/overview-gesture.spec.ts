import { test, expect, type Locator } from '@playwright/test';
import { installMockEngine } from './support/mock-engine';
import { plainReply } from './support/scenarios';
const gesture = (target: Locator, type: string, scale?: number) => target.evaluate((element, value) => {
 const event = new Event(value.type, { bubbles: true, cancelable: true });
 if (value.scale !== undefined) Object.defineProperty(event, 'scale', { value: value.scale });
 element.dispatchEvent(event);
}, { type, scale });
for (const theme of ['light', 'dark'] as const) {
 test(`WebKit spread opens Overview; inward and noise do not · ${theme}`, async ({ page }, info) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, plainReply()); await page.goto('/');
  const strip = page.locator('.workspace-tabstrip');
  const overview = page.getByRole('dialog', { name: 'All tabs overview' });
  await expect(strip).toBeVisible();
  await gesture(strip, 'gesturestart'); await gesture(strip, 'gesturechange', 0.8);
  await expect(overview).not.toBeVisible();
  await gesture(strip, 'gesturechange', 1.05); await expect(overview).not.toBeVisible();
  await gesture(strip, 'gesturechange', 1.2); await expect(overview).toBeVisible();
  if (process.env.GESTURE_SHOTS) await page.screenshot({ path: `${process.env.GESTURE_SHOTS}/overview-spread-${theme}-${info.project.name}.png` });
  await overview.getByRole('button', { name: 'Done' }).click(); await expect(overview).not.toBeVisible();
  await expect(page.getByRole('button', { name: 'All tabs', exact: true })).toBeFocused();
 });
 test(`Ctrl pixel-wheel spread opens Overview; scroll and inward zoom stay unclaimed · ${theme}`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, plainReply()); await page.goto('/');
  const strip = page.locator('.workspace-tabstrip');
  const overview = page.getByRole('dialog', { name: 'All tabs overview' }); await expect(strip).toBeVisible();
  const dispatch = (init: Record<string, unknown>) => strip.evaluate((element, value) => {
   const event = new WheelEvent('wheel', { bubbles: true, cancelable: true, ...value });
   element.dispatchEvent(event); return event.defaultPrevented;
  }, init);
  expect(await dispatch({ deltaY: -30 })).toBe(false);
  expect(await dispatch({ deltaY: 30, ctrlKey: true })).toBe(false);
  expect(await dispatch({ deltaY: -30, ctrlKey: true, deltaMode: 1 })).toBe(false);
  await expect(overview).not.toBeVisible();
  expect(await dispatch({ deltaY: -12, ctrlKey: true })).toBe(true); await expect(overview).toBeVisible();
 });
 test(`content and editing fields retain zoom · ${theme}`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await installMockEngine(page, plainReply()); await page.goto('/');
  const input = page.getByRole('textbox', { name: 'Message', exact: true }); await expect(input).toBeVisible();
  expect(await input.evaluate(element => {
   const event = new WheelEvent('wheel', { bubbles: true, cancelable: true, ctrlKey: true, deltaY: -30 });
   element.dispatchEvent(event); return event.defaultPrevented;
  })).toBe(false);
  await gesture(input, 'gesturestart'); await gesture(input, 'gesturechange', 1.5);
  await gesture(page.locator('.conversation-view'), 'gesturestart');
  await gesture(page.locator('.conversation-view'), 'gesturechange', 1.5);
  await expect(page.getByRole('dialog', { name: 'All tabs overview' })).not.toBeVisible();
 });
 test(`a modal owns input; reduced motion stays instant when the strip opens Overview · ${theme}`, async ({ page }) => {
  await page.addInitScript(value => localStorage.setItem('codeaf-theme', value), theme);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await installMockEngine(page, plainReply()); await page.goto('/');
  const strip = page.locator('.workspace-tabstrip');
  await strip.getByRole('tab').first().dblclick();
  const rename = page.getByRole('dialog', { name: 'Rename tab' });
  await expect(rename).toBeVisible();
  expect(await strip.evaluate(element => {
   const event = new WheelEvent('wheel', { bubbles: true, cancelable: true, ctrlKey: true, deltaY: -30 });
   element.dispatchEvent(event); return event.defaultPrevented;
  })).toBe(false);
  await gesture(strip, 'gesturestart'); await gesture(strip, 'gesturechange', 1.5);
  const overview = page.getByRole('dialog', { name: 'All tabs overview' });
  await expect(overview).not.toBeVisible();
  await rename.getByRole('button', { name: 'Cancel', exact: true }).click();
  await gesture(strip, 'gesturestart'); await gesture(strip, 'gesturechange', 1.5);
  await expect(overview).toBeVisible(); await expect(overview).toHaveCSS('animation-name', 'none');
 });

}

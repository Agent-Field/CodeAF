import { test, expect } from '@playwright/test';
import tauri from '../../src-tauri/tauri.conf.json' with { type: 'json' };

// Forwarded connections must use the bundled engine's origin, rather than
// widening the application's policy to another spelling of loopback.
test('packaged CSP allows the normalized forwarded engine and refuses localhost', async ({ page }) => {
  await page.route(new URL('/', test.info().project.use.baseURL as string).href, async route => {
    await route.fulfill({
      contentType: 'text/html',
      body: '<!doctype html><html><body>Forwarded engine policy fixture</body></html>',
      headers: { 'content-security-policy': tauri.app.security.csp },
    });
  });
  let numericRequests = 0;
  let localhostRequests = 0;
  await page.route('http://127.0.0.1:4321/health', async route => {
    numericRequests++;
    await route.fulfill({ body: 'ready', headers: { 'access-control-allow-origin': '*' } });
  });
  await page.route('http://localhost:4321/health', async route => {
    localhostRequests++;
    await route.fulfill({ body: 'ready', headers: { 'access-control-allow-origin': '*' } });
  });
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const numeric = await fetch('http://127.0.0.1:4321/health').then(response => response.text());
    let localhostBlocked = false;
    try {
      await fetch('http://localhost:4321/health');
    } catch {
      localhostBlocked = true;
    }
    return { numeric, localhostBlocked };
  });
  expect(result).toEqual({ numeric: 'ready', localhostBlocked: true });
  expect(numericRequests).toBe(1);
  expect(localhostRequests).toBe(0);
});

import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';

const fixture = (name: string) => JSON.parse(readFileSync(new URL(`../../src/features/decisions/fixtures/${name}.json`, import.meta.url), 'utf8'));

test('typed clients use the browser engine transport and preserve conflicts', async ({ page }) => {
  const requests: { path: string; method: string; body: unknown }[] = [];
  await page.route('**/api/engine/**', async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace('/api/engine', '');
    if (!req.url().includes('contract-')) { await route.fulfill({ json: {}, status: 501 }); return; }
    requests.push({ path, method: req.method(), body: req.postDataJSON() });
    let name = 'decisions-unavailable';
    let status = 501;
    if (path.endsWith('/knows')) { name = req.method() === 'GET' ? 'knows-list' : 'knows-add'; status = 200; }
    else if (path.includes('/knows/')) { name = path.endsWith('/still-true') ? 'knows-confirm' : req.method() === 'PATCH' ? 'knows-edit' : 'knows-remove'; status = 200; }
    else if (path.includes('/plan/')) { name = path.endsWith('/go') ? 'plan-refusal' : path.endsWith('/edit') ? 'plan-edit' : 'plan-cancel'; status = path.endsWith('/go') ? 409 : 200; }
    else if (path.includes('/councils/')) { name = 'council-refusal'; status = 409; }
    else if (path === '/councils') { name = 'councils-empty'; status = 200; }
    await route.fulfill({ json: fixture(name), status });
  });
  await page.goto('/');
  const result = await page.evaluate(async () => {
    const decisionsPath = '/src/features/decisions/client.ts';
    const knowsPath = '/src/features/places/knows/client.ts';
    const councilsPath = '/src/features/council/client.ts';
    const { createDecisionsClient, createPlanClient } = await import(decisionsPath);
    const { createKnowsClient } = await import(knowsPath);
    const { createCouncilClient } = await import(councilsPath);
    const knows = createKnowsClient();
    const list = await knows.list('contract-place');
    await knows.add('contract-place', { text: 'Run tests', ifRevision: 7 });
    await knows.edit('contract-place', 'contract-line', { text: 'Run focused tests', ifRevision: 7 });
    await knows.confirm('contract-place', 'contract-line', 7);
    await knows.remove('contract-place', 'contract-line', 7);
    const plans = createPlanClient();
    await plans.edit('contract-chat', 'contract-plan', [{ kind: 'stop', target: { task: 't9' }, text: 'Stop task' }]);
    await plans.cancel('contract-chat', 'contract-plan');
    const councils = createCouncilClient();
    const councilList = await councils.list('contract-place');
    const decisions = createDecisionsClient();
    const errors = [];
    for (const op of [
      () => plans.go('contract-chat', 'contract-plan'),
      () => councils.steer('contract-council', 'Wait'),
      () => decisions.list('contract-place'),
      () => decisions.status('contract-place'),
      () => decisions.get('contract-decision'),
      () => decisions.overturn('contract-decision', {}),
      () => decisions.setDecide('contract-place', {}),
    ]) {
      try { await op(); } catch (error) {
        const e = error as { message: string; status: number };
        errors.push({ message: e.message, status: e.status });
      }
    }
    return { list, councilList, errors };
  });
  expect(result.list).toEqual(fixture('knows-list'));
  expect(result.councilList).toEqual(fixture('councils-empty'));
  expect(result.errors).toEqual([
    { message: fixture('plan-refusal').error, status: 409 },
    { message: fixture('council-refusal').error, status: 409 },
    ...Array.from({ length: 5 }, () => ({ message: fixture('decisions-unavailable').error, status: 501 })),
  ]);
  expect(requests).toHaveLength(15);
  expect(requests.find(r => r.method === 'PATCH')?.body).toEqual({ text: 'Run focused tests', ifRevision: 7 });
  expect(requests.find(r => r.method === 'DELETE')?.body).toEqual({ ifRevision: 7 });
  expect(requests.find(r => r.method === 'PUT')?.path).toBe('/places/contract-place/decide');
});

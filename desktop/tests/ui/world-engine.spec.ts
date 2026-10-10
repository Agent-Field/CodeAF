import { expect, test, type Page } from '@playwright/test';
import { createPlacesClient, type PlacesTransport } from '../../src/features/places/client';
import { createUsingClient } from '../../src/features/places/using-client';
import { installWorldEngine } from './support/world-engine';

// Fetch-level smoke for the world mock. d5-qa-nat-multiwindow drives the same
// installWorldEngine, including two pages on one fixture.

function transport(page: Page): PlacesTransport {
  return (path, ask) => page.evaluate(async ({ path, ask }) => {
    const response = await fetch(`/api/engine${path}`, {
      method: ask.method,
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: ask.body === undefined ? undefined : JSON.stringify(ask.body),
    });
    const body = await response.json().catch(() => null);
    if (!response.ok) throw new Error(`${response.status} ${JSON.stringify(body)}`);
    return body;
  }, { path, ask: { method: ask.method, body: ask.body } });
}

async function call(page: Page, path: string, method = 'GET', body?: unknown) {
  return page.evaluate(async ({ path, method, body }) => {
    const response = await fetch(`/api/engine${path}`, {
      method,
      headers: body === null ? { Accept: 'application/json' } : { Accept: 'application/json', 'Content-Type': 'application/json' },
      body: body === null ? undefined : body,
    });
    const text = await response.text();
    return { status: response.status, type: response.headers.get('content-type'), body: text ? JSON.parse(text) : null };
  }, { path, method, body: body === undefined ? null : JSON.stringify(body) });
}

test.beforeEach(async ({ page }) => { await page.goto('/'); });

test('typical day serves world, places, using, settings and jobs', async ({ page }) => {
  const engine = await installWorldEngine(page, 'typicalDay');
  const stream = await page.evaluate(async () => {
    const response = await fetch('/api/engine/events?after=0', { headers: { Accept: 'text/event-stream' } });
    return { type: response.headers.get('content-type'), text: await response.text() };
  });
  expect(stream.type).toContain('text/event-stream');
  const record = JSON.parse(stream.text.split('\n').find(line => line.startsWith('data:'))!.slice(5));
  expect(record.type).toBe('reset');
  expect(record.payload.rows.find((row: { chatId: string }) => row.chatId === 'config-stack').needsYou).toBe(1);

  const graph = await createPlacesClient(transport(page)).graph();
  expect(graph.rail.pinned.map(place => place.name)).toEqual(['codeaf', 'Personal']);
  expect(graph.now.chats).toBe(3);
  const using = await createUsingClient(transport(page)).using('mock-1');
  expect(using.bundle.places.map(place => place.name)).toEqual(['Config parser', 'Software', 'codeaf']);

  expect((await call(page, '/settings/key')).body).toEqual({ present: true, source: 'profile' });
  expect((await call(page, '/settings/permissions', 'PUT', { mode: 'allow' })).body.mode).toBe('allow');
  expect((await call(page, '/sessions/mock-1/jobs')).body[0].state).toBe('running');
  expect((await call(page, '/sessions/mock-1/jobs/1/stop', 'POST', {})).body).toEqual({ accepted: true });
  expect((await call(page, '/sessions/mock-1/jobs')).body[0].state).toBe('stopped');

  expect(engine.counts.events).toBeGreaterThan(0);
  expect(engine.counts.places).toBeGreaterThan(0);
  expect(engine.counts.using).toBeGreaterThan(0);
  expect(engine.counts.settings).toBeGreaterThan(0);
  expect(engine.counts.jobs).toBeGreaterThan(0);
  expect(engine.calls.some(entry => entry.path === '/sessions/mock-1/jobs/1/stop')).toBe(true);
});

test('two windows share one workspace', async ({ page, context }) => {
  const other = await context.newPage();
  await other.goto('/');
  const engine = await installWorldEngine(page, 'typicalDay', [other]);
  const current = await call(page, '/workspaces/now');
  const saved = await call(page, '/workspaces/now', 'PUT', { revision: current.body.revision, writer: 'win-b', workspace: current.body.workspace });
  expect(saved.status).toBe(200);
  const seen = await call(other, '/workspaces/now');
  expect(seen.body.revision).toBe(saved.body.revision);
  expect(seen.body.writer).toBe('win-b');
  const stale = await call(other, '/workspaces/now', 'PUT', { revision: current.body.revision, writer: 'win-c', workspace: current.body.workspace });
  expect(stale.status).toBe(409);
  expect(stale.body.code).toBe('conflict');
  expect(engine.counts.workspaces).toBeGreaterThanOrEqual(4);
});

test('empty is quiet and two hundred places is one graph', async ({ page }) => {
  const empty = await installWorldEngine(page, 'empty');
  expect((await createPlacesClient(transport(page)).graph()).places).toHaveLength(0);
  expect((await call(page, '/settings/key')).body).toEqual({ present: false });
  expect((await call(page, '/sessions/mock-1/using')).status).toBe(404);
  expect(empty.counts.places).toBeGreaterThan(0);

  await page.unroute('**/api/engine/**');
  const wide = await installWorldEngine(page, 'twoHundredPlaces');
  const graph = await createPlacesClient(transport(page)).graph();
  expect(graph.places).toHaveLength(200);
  const reports = graph.places.find(place => place.name === 'Reports');
  expect(reports).toBeTruthy();
  expect((await createPlacesClient(transport(page)).home(reports!.id)).children).toHaveLength(47);
  expect(wide.counts.places).toBeGreaterThan(0);
});

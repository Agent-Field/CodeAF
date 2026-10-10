import { test, expect, type Page } from '@playwright/test';
import { createPlacesClient, type PlacesTransport } from '../../src/features/places/client';
import { createUsingClient } from '../../src/features/places/using-client';
import { installMockEngine } from './support/mock-engine';
import { installPlacesEngine } from './support/places-engine';
import { placesFixture, PLACES_NOW, type PlacesFixtureName } from './support/places-fixture';

/** Browser fetches exercise Playwright routing; the production validators check every answer. */
function transport(page: Page): PlacesTransport {
  return (path, ask) => page.evaluate(async ({ path, ask }) => {
    const response = await fetch(`/api/engine${path}`, { method: ask.method, headers: { 'Content-Type': 'application/json' }, body: ask.body === undefined ? undefined : JSON.stringify(ask.body) });
    if (!response.ok) throw new Error(`${response.status}: ${await response.text()}`);
    return response.json();
  }, { path, ask: { method: ask.method, body: ask.body } });
}

test('PL-011: all named fixtures have deterministic counts, depth and a bounded rail', async ({ page }) => {
  await installMockEngine(page, { initial: {} });
  await page.goto('/');
  const names: PlacesFixtureName[] = ['empty', 'first-launch', 'typical-day', 'reports', '200-places', 'closed-but-running', 'failed'];
  for (const name of names) {
    expect(placesFixture(name)).toEqual(placesFixture(name));
    const engine = await installPlacesEngine(page, name);
    const client = createPlacesClient(transport(page));
    const graph = await client.graph();
    expect(graph.readAt).toBe(PLACES_NOW);
    expect(graph.rail.pinned.length + graph.rail.open.length).toBeLessThanOrEqual(5);
    if (name === 'empty') expect(graph.totals).toMatchObject({ places: 0, unplaced: 0 });
    if (name === 'first-launch') expect(graph.totals).toMatchObject({ places: 0, unplaced: 2 });
    if (name === 'typical-day') {
      expect(graph.rail.pinned.map(p => p.name)).toEqual(['codeaf', 'Personal']);
      expect(graph.rail.open.map(p => p.name)).toEqual(['Config parser', 'Marketing', 'Q3 report']);
      expect(graph.now.chats).toBe(3);
    }
    if (name === 'reports' || name === '200-places') expect((await client.home('reports')).children).toHaveLength(47);
    if (name === '200-places') {
      expect(graph.places).toHaveLength(200);
      const depth = (id: string): number => { const p = graph.places.find(p => p.id === id)!; return 1 + Math.max(0, ...p.parents.map(depth)); };
      expect(Math.max(...graph.places.map(p => depth(p.id)))).toBe(3);
    }
    if (name === 'closed-but-running') {
      expect(graph.rail.open.find(p => p.id === 'q3-report')).toMatchObject({ closed: true, status: { running: 1 } });
      engine.setChat('q3-chat', { live: false, tasks: { running: 0, done: 1 } });
      expect((await client.rail()).open.map(p => p.id)).not.toContain('q3-report');
    }
    if (name === 'failed') expect((await client.status()).places.reports.statusInclusive.failedTasks).toBe(1);
  }
});

test('CRUD, membership, sources, undo and stale generations use the canonical wire', async ({ page }) => {
  await installMockEngine(page, { initial: {} });
  const engine = await installPlacesEngine(page, 'first-launch');
  await page.goto('/');
  const client = createPlacesClient(transport(page));
  const created = await client.createPlace({ name: 'Tests', tint: 'tide' });
  const id = created.place!.id;
  await client.updatePlace(id, { name: 'Test fixtures', instructions: 'Use deterministic clocks.' });
  await client.addChats(id, ['loose-1']);
  expect((await client.home(id)).chats.map(chat => chat.id)).toEqual(['loose-1']);
  await client.pinPlace(id);
  expect((await client.rail()).pinned.map(p => p.id)).toContain(id);
  await client.railOp({ op: 'unpin', place: id });
  await client.railOp({ op: 'visit', place: id });
  expect((await client.rail()).open.map(p => p.id)).toContain(id);
  await client.railOp({ op: 'close', place: id });
  expect((await client.rail()).open.map(p => p.id)).not.toContain(id);
  const source = await client.addSource(id, { kind: 'url', ref: 'https://example.com/fixtures' });
  expect((await client.home(id)).place!.sources).toHaveLength(1);
  await client.undo(source.undo);
  expect((await client.home(id)).place!.sources).toHaveLength(0);
  await expect(client.updatePlace(id, { name: 'Stale', ifGeneration: 0 })).rejects.toThrow('409');
  await client.archivePlace(id);
  expect((await client.graph()).places).toHaveLength(0);
  await client.restorePlace(id);
  await client.deletePlace(id);
  expect((await client.graph()).now.chats).toBe(2);
  expect(engine.posts('/members')).toHaveLength(1);
});

test('Using resolves two ancestor levels and world records report real fixture work', async ({ page }) => {
  await installMockEngine(page, { initial: {} });
  const seed = placesFixture('typical-day');
  seed.sessions = { 'session-token': 'config-stack' };
  const engine = await installPlacesEngine(page, seed);
  await page.goto('/');
  const using = createUsingClient(transport(page));
  const view = await using.using('session-token');
  expect(view.bundle.places.map(p => p.id)).toEqual(['config-parser', 'software', 'codeaf']);
  expect(view.bundle.sources).toHaveLength(1);
  expect(view.bundle.instructions[0].text).toBe('Keep changes focused.');
  const world = await page.evaluate(async () => (await fetch('/api/engine/world')).json());
  expect(world.rows.find((row: { session: string }) => row.session === 'config-stack')).toMatchObject({ running: true, needsYou: true });
  engine.nudge();
  const stream = await page.evaluate(async () => (await fetch('/api/engine/events?after=1')).text());
  expect(stream).toContain('"type":"world"');
  expect(stream).toContain(PLACES_NOW);
  view.bundle.policy = [{ field: 'model', value: '', outcome: 'needsPick', wanted: [{ placeId: 'software', value: 'fixture-model' }] }];
  view.settings = [{ field: 'model', state: 'needsPick', reason: 'Choose a place.' }];
  engine.setUsing('session-token', view);
  await expect(using.choose('session-token', 'model', 'missing')).rejects.toThrow('422');
  expect((await using.choose('session-token', 'model', 'software')).settings[0].state).toBe('pending');
  expect((await using.apply('session-token', 'model')).settings[0].state).toBe('yours');
});

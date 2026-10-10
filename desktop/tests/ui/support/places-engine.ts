import type { Page, Route } from '@playwright/test';
import type { UsingView, UsedPlace, UsedSource } from '../../../src/features/places/using-client';
import type { Tint } from '../../../src/features/places/client';
import { installMockPlaces, type MockPlaces } from './mock-places';
import { placesFixture, type PlacesFixture, type PlacesFixtureName } from './places-fixture';

export type PlacesEngine = MockPlaces & {
  using: (chatId: string) => UsingView;
  setUsing: (token: string, view: UsingView) => void;
};

/**
 * Install after the conversation mock when a spec wants its own graph. installMockEngine mounts one itself
 * and falls through to it; a later call still wins, because Playwright tries the newest route first.
 * `onChange` fires after a graph write so that owner can publish a places record on the world stream.
 */
export async function installPlacesEngine(page: Page, fixture: PlacesFixtureName | PlacesFixture = 'typical-day', alsoServe: Page[] = [], hooks?: { onChange?: () => void }): Promise<PlacesEngine> {
  const seed = typeof fixture === 'string' ? placesFixture(fixture) : structuredClone(fixture);
  const mock = await installMockPlaces(page, seed, alsoServe, hooks);
  const overrides = structuredClone(seed.using ?? {});
  const using = (chatId: string): UsingView => {
    if (overrides[chatId]) return structuredClone(overrides[chatId]);
    const state = mock.state();
    const places: UsedPlace[] = [];
    const levels = new Map<string, number>();
    const queue = state.members.filter(member => member.chatId === chatId).map(member => ({ id: member.placeId, level: 0 }));
    for (let index = 0; index < queue.length; index++) {
      const item = queue[index];
      if (item.level > 2 || levels.has(item.id)) continue;
      const place = state.places.find(place => place.id === item.id && !place.archived);
      if (!place) continue;
      levels.set(item.id, item.level);
      const tintOf = (id: string, seen = new Set<string>()): Tint => {
        const p = state.places.find(place => place.id === id);
        if (!p || seen.has(id)) return 'graphite';
        seen.add(id);
        return p.tint || tintOf(p.parents[0], seen);
      };
      places.push({ id: item.id, name: place.name, tint: tintOf(item.id), level: item.level, inherited: item.level > 0 });
      queue.push(...place.parents.map(id => ({ id, level: item.level + 1 })));
    }
    const sources: UsedSource[] = [];
    const instructions: UsingView['bundle']['instructions'] = [];
    for (const used of places) {
      const place = state.places.find(place => place.id === used.id)!;
      if (place.instructions) instructions.push({ placeId: used.id, text: place.instructions, bytes: new TextEncoder().encode(place.instructions).length });
      for (const source of place.sources) {
        const key = `${source.kind}:${source.ref}`;
        let row = sources.find(row => row.key === key);
        if (!row) { row = { key, kind: source.kind, ref: source.ref, label: source.label, status: 'ok', from: [] }; sources.push(row); }
        row.from.push({ placeId: used.id, sourceId: source.id, addedBy: source.addedBy, at: source.at, level: used.level });
      }
    }
    // Policy edge cases are explicit engine answers in seed.using, never guessed by a fixture resolver.
    return {
      chatId, engine: { places: true }, revision: state.revision, readAt: seed.now, settings: [],
      bundle: { chatId, revision: state.revision, places, instructions, sources, trimmed: [], refused: [], policy: [], counts: { places: places.length, sources: sources.length } },
    };
  };
  const serveUsing = async (route: Route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace(/^.*\/api\/engine/, '');
    const match = /^\/sessions\/([^/]+)\/using(?:\/(choice|apply))?$/.exec(path);
    if (!match) return route.fallback();
    const token = decodeURIComponent(match[1]);
    const chatId = seed.sessions?.[token] ?? token;
    const state = mock.state();
    const json = (value: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) });
    if (!state.chats.some(chat => chat.id === chatId) && !overrides[token]) return json({ error: 'That conversation is not attached.', code: 'not_found' }, 404);
    let body: Record<string, unknown> = {};
    try { body = request.postDataJSON() ?? {}; } catch { return json({ error: 'That request was not JSON.', code: 'invalid' }, 400); }
    mock.calls.push({ method: request.method(), path, body });
    const view = overrides[token] ? structuredClone(overrides[token]) : using(chatId);
    if (request.method() === 'GET' && !match[2]) return json(view);
    if (request.method() !== 'POST' || !match[2]) return json({ error: 'Method not allowed.' }, 405);
    if (body.ifGeneration !== undefined && body.ifGeneration !== state.revision) return json({ error: 'The places changed. Look again and retry.', code: 'stale' }, 409);
    const decision = view.bundle.policy.find(decision => decision.field === body.field);
    const setting = view.settings.find(setting => setting.field === body.field);
    if (!decision || !setting) return json({ error: 'That field has no place setting.', code: 'not_a_candidate' }, 422);
    if (match[2] === 'choice') {
      const candidate = decision.wanted.find(want => want.placeId === body.placeId);
      if (!candidate) return json({ error: 'That place is not a candidate.', code: 'not_a_candidate' }, 422);
      decision.value = candidate.value;
      decision.outcome = 'chosen';
      decision.decidedBy = 'you';
      decision.chosen = candidate.placeId;
      Object.assign(setting, { state: 'pending', value: candidate.value, decidedBy: 'you' });
    } else {
      if (!decision.value) return json({ error: 'Choose a place first.', code: 'needs_pick' }, 422);
      Object.assign(setting, { state: 'yours', value: decision.value, current: decision.value, reason: 'You chose this conversation’s setting.' });
    }
    overrides[token] = view;
    mock.nudge();
    return json(view);
  };
  for (const window of [page, ...alsoServe]) await window.route('**/api/engine/sessions/**/using**', serveUsing);
  return { ...mock, using, setUsing: (token, view) => { overrides[token] = structuredClone(view); mock.nudge(); } };
}

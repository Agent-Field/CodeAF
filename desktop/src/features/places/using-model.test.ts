import test from 'node:test';
import assert from 'node:assert/strict';
import { applyOffer, chipText, handoffFor, placeList, provenance, safeUrl, sourceName, tallyLine, wantedLine } from './using-model.ts';
import type { PlaceSetting, PolicyDecision, UsedSource, UsingBundle } from './using-types.ts';

// The shapes mirror what the bridge sends; the names are invented for the tests.
const origin = (placeId: string, addedBy: 'you' | 'ai' = 'you') => ({ placeId, sourceId: `s-${placeId}`, addedBy, level: 0 });
const source = (patch: Partial<UsedSource>): UsedSource => ({ key: 'k', kind: 'file', ref: '/work/a.md', from: [origin('p1')], status: 'ok', ...patch });
const bundle = (patch: Partial<UsingBundle> = {}): UsingBundle => ({
  chatId: 'c', revision: 1,
  places: [{ id: 'p1', name: 'Release', tint: 'tide', level: 0, inherited: false }, { id: 'p2', name: 'Marketing', tint: 'rose', level: 0, inherited: false }],
  instructions: [], sources: [], trimmed: [], refused: [], policy: [], counts: { places: 2, sources: 0 }, ...patch,
});

test('chip text counts places and sources, and leaves zero sources unsaid', () => {
  assert.equal(chipText(bundle()), 'Using 2 places');
  assert.equal(chipText(bundle({ counts: { places: 1, sources: 1 } })), 'Using 1 place · 1 source');
  assert.equal(chipText(bundle({ counts: { places: 3, sources: 4 } })), 'Using 3 places · 4 sources');
});

test('a source opens only when it is real: never missing or refused, and a url must be http(s)', () => {
  assert.deepEqual(handoffFor(source({})), { kind: 'file', path: '/work/a.md', repoRoot: undefined });
  assert.deepEqual(handoffFor(source({ kind: 'repo', ref: '/work/r', repoRoot: '/work' })), { kind: 'repo', path: '/work/r', repoRoot: '/work' });
  assert.deepEqual(handoffFor(source({ kind: 'chat', ref: 'chat-9' })), { kind: 'chat', chatId: 'chat-9' });
  assert.deepEqual(handoffFor(source({ kind: 'url', ref: 'https://codeaf.dev/x' })), { kind: 'url', url: 'https://codeaf.dev/x' });
  assert.equal(handoffFor(source({ kind: 'url', ref: 'javascript:alert(1)' })), undefined);
  assert.equal(handoffFor(source({ kind: 'url', ref: 'file:///etc/passwd' })), undefined);
  assert.equal(handoffFor(source({ status: 'missing' })), undefined);
  assert.equal(handoffFor(source({ status: 'refused' })), undefined);
  assert.equal(safeUrl('not a url'), undefined);
});

test('names: the label first, then the tail of the path, then the url without its scheme', () => {
  assert.equal(sourceName(source({ label: 'Voice' })), 'Voice');
  assert.equal(sourceName(source({ kind: 'folder', ref: '/work/codeaf/' })), 'codeaf');
  assert.equal(sourceName(source({ kind: 'url', ref: 'https://codeaf.dev/changelog' })), 'codeaf.dev/changelog');
});

test('provenance names each place once, with the AI marked, and never prints an id', () => {
  const rows = provenance(source({ from: [origin('p1'), origin('p1'), origin('p2', 'ai'), origin('gone')] }), bundle());
  assert.deepEqual(rows, [{ place: 'Release', by: 'you' }, { place: 'Marketing', by: 'ai' }, { place: 'Another place', by: 'you' }]);
  assert.equal(placeList(['A', 'B', 'C', 'D']), 'A, B and 2 more');
  assert.equal(placeList(['A', 'A']), 'A');
});

test('the tally says only what is true of the bundle', () => {
  assert.equal(tallyLine(bundle()), undefined);
  const tally = tallyLine(bundle({ sources: [source({ status: 'missing' }), source({})], trimmed: [source({}), source({})], refused: [source({ status: 'refused' })] }));
  assert.equal(tally, '2 sources left out for room · 1 refused · 1 not found');
});

test('apply is offered only for the two states a person can resolve, and a wider permission asks twice', () => {
  const setting = (patch: Partial<PlaceSetting>): PlaceSetting => ({ field: 'permissions', state: 'needsYou', value: 'allow', ...patch });
  assert.deepEqual(applyOffer(setting({})), { confirm: true });
  assert.deepEqual(applyOffer(setting({ field: 'model', state: 'notNew', value: 'pro' })), { confirm: false });
  for (const state of ['applied', 'pending', 'needsPick', 'yours', 'unavailable'] as const) assert.equal(applyOffer(setting({ state })), undefined);
  assert.equal(applyOffer(setting({ value: undefined })), undefined);
  assert.equal(applyOffer(undefined), undefined);
});

test('the places\' wishes read as the design says: who wanted what, and who decided', () => {
  const decision = (patch: Partial<PolicyDecision>): PolicyDecision => ({ field: 'model', value: 'pro', outcome: 'decided', decidedBy: 'p2', wanted: [{ placeId: 'p1', value: 'flash' }], ...patch });
  assert.equal(wantedLine(decision({}), bundle()), 'Release wanted flash · Marketing decided');
  assert.equal(wantedLine(decision({ outcome: 'chosen', decidedBy: 'you' }), bundle()), 'Release wanted flash · You picked');
  assert.equal(wantedLine(decision({ outcome: 'needsPick', value: '', decidedBy: undefined, wanted: [{ placeId: 'p1', value: 'flash' }, { placeId: 'p2', value: 'pro' }] }), bundle()), 'Release wanted flash · Marketing wanted pro');
  assert.equal(wantedLine(decision({ outcome: 'agreed', wanted: [{ placeId: 'p1', value: 'pro' }, { placeId: 'p2', value: 'pro' }] }), bundle()), undefined);
});

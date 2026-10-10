// TEST-ONLY MOCK. Everything here is invented for the browser tests; it is never a person's conversation, and the page that
// mounts it says so. It follows the shape and the rules of the bridge's Using routes (internal/desktopbridge/using.go), nothing more.
import { PlacesError } from '../../../src/features/places/client';
import type { PlaceSetting, PolicyField, UsingApi, UsingView } from '../../../src/features/places/using-types';

export type Scenario = 'full' | 'quiet' | 'empty' | 'offline' | 'failing' | 'noengine' | 'absent' | 'slow' | 'single' | 'decided';

const origin = (placeId: string, sourceId: string, level = 0, addedBy: 'you' | 'ai' = 'you') => ({ placeId, sourceId, addedBy, level });

export function fullView(): UsingView {
  return {
    chatId: 'chat-mock-1757', revision: 7, readAt: '2026-10-09T10:00:00Z',
    engine: { places: true },
    bundle: {
      chatId: 'chat-mock-1757', revision: 7,
      places: [
        { id: 'pl_parser', name: 'Config parser', tint: 'tide', level: 0, inherited: false },
        { id: 'pl_release', name: 'Release', tint: 'tide', level: 0, inherited: false },
        { id: 'pl_codeaf', name: 'codeaf', tint: 'tide', level: 1, inherited: true, through: ['pl_release'] },
      ],
      instructions: [
        { placeId: 'pl_parser', text: 'Keep strict mode the default for public APIs', bytes: 44 },
        { placeId: 'pl_release', text: 'Write for customers, not engineers. Lead with what changes for them, then the reason, and link the changelog instead of repeating it here.', bytes: 140, trimmed: true },
      ],
      // The popover lists these lines. The instruction prose above is what the prompt was given, including the cut.
      knows: [
        { id: 'k-strict', placeId: 'pl_parser', text: 'Keep strict mode the default for public APIs', source: { kind: 'you-wrote' }, createdAt: '2026-10-01T12:00:00Z' },
        { id: 'k-customers', placeId: 'pl_release', text: 'Write for customers, not engineers. Lead with what changes for them, then the reason, and link the changelog instead of repeating it here.', source: { kind: 'learned', answers: 3 }, createdAt: '2026-10-08T12:00:00Z', lastUsedAt: '2026-10-09T12:00:00Z' },
        { id: 'k-old', placeId: 'pl_release', text: 'Lead with internal names', source: { kind: 'you-wrote' }, createdAt: '2026-10-01T12:00:00Z', replacedBy: 'k-customers', replacedAt: new Date(Date.now() - 2 * 86_400_000).toISOString() },
      ],
      sources: [
        { key: 'repo:/work/codeaf/internal/parse', kind: 'repo', ref: '/work/codeaf/internal/parse', repoRoot: '/work/codeaf', from: [origin('pl_parser', 's1')], status: 'ok' },
        { key: 'file:/work/brand-voice.md', kind: 'file', ref: '/work/brand-voice.md', label: 'brand-voice.md', from: [origin('pl_release', 's2', 0, 'ai')], status: 'ok' },
        { key: 'file:/work/old-notes.md', kind: 'file', ref: '/work/old-notes.md', from: [origin('pl_release', 's3')], status: 'missing', reason: 'This file is no longer at /work/old-notes.md.' },
        { key: 'url:https://codeaf.dev/changelog', kind: 'url', ref: 'https://codeaf.dev/changelog', from: [origin('pl_codeaf', 's4', 1)], status: 'ok' },
        { key: 'chat:chat-earlier', kind: 'chat', ref: 'chat-earlier', label: 'Config stack', from: [origin('pl_parser', 's5')], status: 'ok' },
      ],
      trimmed: [{ key: 'folder:/work/archive', kind: 'folder', ref: '/work/archive', from: [origin('pl_codeaf', 's6', 1)], status: 'ok' }],
      refused: [{ key: 'folder:/home/me/.codeaf', kind: 'folder', ref: '/home/me/.codeaf', from: [origin('pl_release', 's7')], status: 'refused', reason: 'The codeaf folder holds credentials, so it is never given to a model.' }],
      policy: [
        { field: 'model', value: '', outcome: 'needsPick', wanted: [{ placeId: 'pl_release', value: 'flash' }, { placeId: 'pl_parser', value: 'pro' }] },
        { field: 'permissions', value: 'allow', outcome: 'decided', decidedBy: 'pl_codeaf', wanted: [{ placeId: 'pl_release', value: 'allow' }, { placeId: 'pl_parser', value: 'ask' }] },
      ],
      counts: { places: 3, sources: 5 },
    },
    settings: [
      { field: 'model', state: 'needsPick', current: 'deepseek/deepseek-v4.1-flash', reason: 'Release and Config parser want different models, so none is applied until you pick.' },
      { field: 'permissions', state: 'needsYou', value: 'allow', current: 'ask', decidedBy: 'pl_codeaf', reason: 'codeaf allows more than this conversation does now, so it is held until you say so.' },
    ],
  };
}

export function quietView(): UsingView {
  const view = fullView();
  view.bundle.sources = view.bundle.sources.filter(source => source.status === 'ok').slice(0, 2);
  view.bundle.trimmed = []; view.bundle.refused = [];
  view.bundle.counts.sources = view.bundle.sources.length;
  view.bundle.instructions = view.bundle.instructions.map(item => ({ ...item, trimmed: false }));
  view.bundle.policy = [{ field: 'model', value: 'pro', outcome: 'agreed', wanted: [{ placeId: 'pl_release', value: 'pro' }] }];
  view.settings = [{ field: 'model', state: 'applied', value: 'pro', current: 'pro' }];
  return view;
}

/** One place and nothing else in use: the header chip is that place's swatch and name (Places 9b). */
export function singleView(): UsingView {
  const view = quietView();
  view.bundle.places = view.bundle.places.slice(0, 1);
  view.bundle.sources = [];
  view.bundle.counts = { places: 1, sources: 0 };
  return view;
}

export type Calls = { kind: 'using' | 'choose' | 'apply'; field?: PolicyField; placeId?: string }[];

/** An in-memory bridge: reads the scenario's list, and applies a choice or an apply by the engine's rules (409 / 422 included). */
export function mockApi(scenario: Scenario, calls: Calls, flags: { choiceFails?: boolean } = {}): UsingApi | undefined {
  if (scenario === 'absent') return undefined;
  let view = scenario === 'empty' ? { ...quietView(), bundle: { ...quietView().bundle, places: [], sources: [], instructions: [], knows: [], policy: [], counts: { places: 0, sources: 0 } }, settings: [] }
    : scenario === 'single' ? singleView()
    : scenario === 'quiet' ? quietView() : scenario === 'noengine' ? { ...fullView(), engine: { places: false, reason: 'This conversation was opened by another program, which reads no places.' }, settings: fullView().settings.map(s => ({ ...s, state: 'unavailable' as const })) }
    : fullView();
  if (scenario === 'decided') {
    view.bundle.policy = [{ field: 'model', value: 'Pro', outcome: 'decided', decidedBy: 'pl_codeaf', wanted: [{ placeId: 'pl_release', value: 'Flash' }, { placeId: 'pl_parser', value: 'Pro' }] }];
    view.settings = [{ field: 'model', state: 'applied', value: 'Pro', current: 'Pro' }];
  }
  let broken = scenario === 'offline' || scenario === 'failing';
  const set = (field: PolicyField, next: PlaceSetting) => { view = { ...view, revision: view.revision + 1, bundle: { ...view.bundle, revision: view.revision + 1 }, settings: view.settings.map(s => (s.field === field ? next : s)) }; };
  return {
    async using(_token, signal) {
      calls.push({ kind: 'using' });
      if (scenario === 'slow') await new Promise(resolve => setTimeout(resolve, 600));
      if (signal?.aborted) throw new DOMException('aborted', 'AbortError');
      if (scenario === 'offline' && broken) throw new PlacesError('Nothing answered.', 0, '', [], undefined, true);
      if (scenario === 'failing' && broken) throw new PlacesError('The engine could not read the places file.', 500, 'read_failed');
      return structuredClone(view);
    },
    async choose(_token, field, placeId) {
      calls.push({ kind: 'choose', field, placeId });
      if (flags.choiceFails) throw new PlacesError('That choice is no longer needed: the places agree now.', 409, 'no_conflict');
      const decision = view.bundle.policy.find(d => d.field === field);
      const want = decision?.wanted.find(w => w.placeId === placeId);
      if (!decision || !want) throw new PlacesError('That place is not one of the candidates.', 422, 'not_a_candidate');
      decision.outcome = 'chosen'; decision.chosen = placeId; decision.decidedBy = 'you'; decision.value = want.value;
      // The engine applies a remembered pick at the next turn, so the state it answers with is pending.
      set(field, { field, state: 'pending', value: want.value, current: 'deepseek/deepseek-v4.1-flash', decidedBy: 'you' });
      return structuredClone(view);
    },
    async apply(_token, field) {
      calls.push({ kind: 'apply', field });
      const setting = view.settings.find(s => s.field === field);
      if (!setting || !setting.value) throw new PlacesError('Nothing to apply.', 409, 'nothing_to_apply');
      set(field, { field, state: 'yours', value: setting.value, current: setting.value, reason: 'You set what runs without asking in this conversation yourself, so places do not change it.' });
      return structuredClone(view);
    },
    // Lets a test heal the scenario so "Try again" can succeed.
    ...(scenario === 'offline' || scenario === 'failing' ? { heal: () => { broken = false; } } : {}),
  } as UsingApi;
}

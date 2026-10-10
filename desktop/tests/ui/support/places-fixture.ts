// These deterministic specimens stay in tests; the renderer never imports them.
import type { PlacesSeed, SeedPlace } from './mock-places';
import type { UsingView } from '../../../src/features/places/using-client';

export const PLACES_NOW = '2026-10-10T12:00:00.000Z';
export type PlacesFixtureName = 'empty' | 'first-launch' | 'typical-day' | 'reports' | '200-places' | 'closed-but-running' | 'failed';
export type PlacesFixture = PlacesSeed & {
  now: string;
  closed?: string[];
  /** Explicit overrides let Using specs exercise pending, refused and conflict answers. */
  using?: Record<string, UsingView>;
  /** Session tokens and stored chat ids differ on the real bridge. */
  sessions?: Record<string, string>;
};

const place = (id: string, name: string, extra: Partial<SeedPlace> = {}): SeedPlace => ({ id, name, ...extra });

/** Each call owns fresh arrays so a spec's mutations cannot contaminate another spec. */
export function placesFixture(name: PlacesFixtureName = 'typical-day'): PlacesFixture {
  const seed: PlacesFixture = { now: PLACES_NOW, places: [], chats: [], disk: ['/work/codeaf', '/work/brand-voice.md'] };
  if (name === 'empty') return seed;
  if (name === 'first-launch') {
    seed.chats = [{ id: 'loose-1', title: 'Explain Go generics constraints' }, { id: 'loose-2', title: 'Plan the week' }];
    return seed;
  }
  seed.places = [
    place('codeaf', 'codeaf', { tint: 'tide', pinned: true, instructions: 'Keep changes focused.', sources: [{ kind: 'folder', ref: '/work/codeaf' }] }),
    place('personal', 'Personal', { tint: 'sand', pinned: true }),
    place('software', 'Software', { parents: ['codeaf'] }),
    place('config-parser', 'Config parser', { parents: ['codeaf'], lastOpenedAt: 'now' }),
    place('marketing', 'Marketing', { parents: ['codeaf'], tint: 'rose', lastOpenedAt: '2026-10-10T11:00:00.000Z', sources: [{ kind: 'file', ref: '/work/brand-voice.md' }] }),
    place('reports', 'Reports', { tint: 'sage' }),
    place('q3-report', 'Q3 report', { parents: ['reports'], lastOpenedAt: '2026-10-10T10:00:00.000Z' }),
    place('reading', 'Reading', { tint: 'iris' }),
    place('release', 'Release', { parents: ['software', 'marketing'] }),
  ];
  seed.chats = [
    { id: 'config-stack', title: 'Trailing commas across the config stack', places: ['config-parser', 'software'], live: true, tasks: { running: 4 }, needsYou: true, reason: 'Choose the rollout scope' },
    { id: 'launch-copy', title: 'Launch copy', places: ['marketing'] },
    { id: 'q3-chat', title: 'Q3 report', places: ['q3-report'] },
    ...Array.from({ length: 3 }, (_, i) => ({ id: `loose-${i + 1}`, title: ['Pricing for teams', 'Explain Go generics constraints', 'Plan the week'][i] })),
  ];
  if (name === 'closed-but-running') {
    seed.closed = ['q3-report'];
    seed.chats!.find(chat => chat.id === 'q3-chat')!.live = true;
    seed.chats!.find(chat => chat.id === 'q3-chat')!.tasks = { running: 1 };
  }
  if (name === 'failed') seed.chats!.find(chat => chat.id === 'q3-chat')!.tasks = { failed: 1, incomplete: 1 };
  if (name === 'reports' || name === '200-places') {
    // Q3 is already present; these 46 siblings make Reports have exactly 47 children.
    seed.places.push(...Array.from({ length: 46 }, (_, i) => place(`report-${i + 1}`, i === 0 ? 'Q2 report' : i === 1 ? 'Churn deep-dive' : `Report ${i + 1}`, { parents: ['reports'] })));
  }
  if (name === '200-places') {
    // Root, child and grandchild are three levels. Only the typical working set is visited.
    const remaining = 200 - seed.places.length;
    seed.places.push(...Array.from({ length: remaining }, (_, i) => place(`scale-${i + 1}`, `Project ${String(i + 1).padStart(3, '0')}`, { parents: [i % 2 ? 'software' : 'personal'] })));
  }
  return seed;
}

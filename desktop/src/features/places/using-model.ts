/**
 * What the Using list says, derived from the engine's answer and nothing else.
 *
 * No function here reads a file, fetches a url or interprets a place's context:
 * the bundle arrives resolved. The words are the only thing decided here, and
 * every state word comes from `settings[].state`, the one rule the engine runs
 * at the start of a turn.
 */

import type { SourceKind } from './client.ts';
import { knowsList, type KnowsLine } from './knows/model.ts';
import type {
  PlaceSetting, PolicyDecision, PolicyField, SettingState, SourceHandoff, UsedInstruction, UsedSource, UsingBundle, UsingKnowsLine, UsingView, Want,
} from './using-types.ts';

export const fieldLabel: Record<PolicyField, string> = { model: 'Model', permissions: 'Permissions' };

const permissionWords: Record<string, string> = { ask: 'Ask', guardian: 'Guardian', allow: 'Allow', deny: 'Deny', auto: 'Auto' };
/** A permissions word reads as the settings page spells it; a model id and any unknown word are shown exactly as sent. */
export const valueLabel = (field: PolicyField, value: string): string => (field === 'permissions' ? permissionWords[value] ?? value : value);

const plural = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`;

/** "Using 3 places · 4 sources". Zero sources is left unsaid rather than drawn as "0 sources". */
export function chipText(bundle: UsingBundle): string {
  const places = plural(bundle.counts.places, 'place', 'places');
  return bundle.counts.sources > 0 ? `Using ${places} · ${plural(bundle.counts.sources, 'source', 'sources')}` : `Using ${places}`;
}

/** The chip draws only when there is something to say: unknown or zero renders as nothing. */
export const hasContext = (view: UsingView): boolean => view.bundle.counts.places > 0 || view.bundle.counts.sources > 0;

/** A place's name from the bundle; 'you' is a remembered pick. An id the bundle does not carry is never shown raw. */
export function placeName(bundle: UsingBundle, id: string | undefined): string {
  if (id === 'you') return 'You';
  return bundle.places.find(place => place.id === id)?.name ?? 'Another place';
}

/** Where a place sits in the chain: nearest parents first, then what they inherit. */
export function levelWords(place: UsingBundle['places'][number], bundle: UsingBundle): string {
  if (!place.inherited) return '';
  const through = (place.through ?? []).map(id => placeName(bundle, id));
  return through.length > 0 ? `inherited through ${through.join(', ')}` : 'inherited';
}

/** The place or places a source came from, with who added it. The same source filed in two places names both. */
export function provenance(source: UsedSource, bundle: UsingBundle): { place: string; by: 'you' | 'ai' }[] {
  const seen = new Set<string>();
  const rows: { place: string; by: 'you' | 'ai' }[] = [];
  for (const origin of source.from) {
    const key = `${origin.placeId}:${origin.addedBy}`;
    if (seen.has(key)) continue;
    seen.add(key);
    rows.push({ place: placeName(bundle, origin.placeId), by: origin.addedBy });
  }
  return rows;
}

/** "Release", "Release, Software" or "Release and 2 more": the aside on a row. */
export function placeList(names: readonly string[], room = 2): string {
  const unique = [...new Set(names)];
  if (unique.length <= room) return unique.join(', ');
  return `${unique.slice(0, room).join(', ')} and ${unique.length - room} more`;
}

const pathTail = (ref: string) => ref.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || ref;

/** The row's name: the label the person gave it, else the last part of the path, else the url without its scheme. */
export function sourceName(source: UsedSource): string {
  if (source.label) return source.label;
  if (source.kind === 'url') return source.ref.replace(/^https?:\/\//i, '');
  if (source.kind === 'chat') return 'A conversation';
  return pathTail(source.ref);
}

export function sourceDetail(source: UsedSource): string | undefined {
  if (source.kind === 'url') return undefined;
  if (source.kind === 'chat') return undefined;
  return source.ref;
}

/** An http(s) url only; anything else is left unopenable. */
export function safeUrl(ref: string): string | undefined {
  try {
    const url = new URL(ref);
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : undefined;
  } catch { return undefined; }
}

/**
 * The typed handoff for opening one source, or undefined when it cannot be opened:
 * a file that is missing has nothing to open, a refused source was never given, and a url
 * that is not http(s) is not followed.
 */
export function handoffFor(source: UsedSource): SourceHandoff | undefined {
  if (source.status !== 'ok') return undefined;
  const kind: SourceKind = source.kind;
  if (kind === 'url') { const url = safeUrl(source.ref); return url ? { kind, url } : undefined; }
  if (kind === 'chat') return { kind, chatId: source.ref };
  return { kind, path: source.ref, repoRoot: source.repoRoot };
}

/** Counts that are true to the bundle: what was given, what was left out for room, what was refused. */
export function sourceTally(bundle: UsingBundle): { given: number; leftOut: number; refused: number; missing: number } {
  return {
    given: bundle.sources.length,
    leftOut: bundle.trimmed.length,
    refused: bundle.refused.length,
    missing: bundle.sources.filter(source => source.status === 'missing').length,
  };
}

/** One sentence under the Sources label when anything was not given as asked; undefined when everything was. */
export function tallyLine(bundle: UsingBundle): string | undefined {
  const { leftOut, refused, missing } = sourceTally(bundle);
  const parts: string[] = [];
  if (leftOut > 0) parts.push(`${plural(leftOut, 'source', 'sources')} left out for room`);
  if (refused > 0) parts.push(`${refused} refused`);
  if (missing > 0) parts.push(`${missing} not found`);
  return parts.length > 0 ? parts.join(' · ') : undefined;
}

/** How many instructions were cut to fit; the model reads the rest. */
export const trimmedInstructions = (bundle: UsingBundle): number => bundle.instructions.filter(item => item.trimmed).length;

/** One row of the Using sheet's Instructions group. */
export type InstructionRow = {
  key: string;
  text: string;
  /** The place or places the row is credited to, already named. */
  places: string;
  /** The knows caption (learned, replaced, you added, a file). Empty when there is nothing true to say. */
  source: string;
  struck: boolean;
  trimmed: boolean;
};

function asKnows(line: UsingKnowsLine): KnowsLine {
  return line;
}

/** Home spells a line the person wrote as "you added"; the same words are used here so the two lists do not drift. */
function knowsCaption(line: KnowsLine, source: string, struck: boolean): string {
  if (line.source.kind === 'you-wrote' && !struck) return 'you added';
  return source;
}

function proseRow(item: UsedInstruction, bundle: UsingBundle, index: number): InstructionRow {
  const from = [placeName(bundle, item.placeId), ...(item.alsoFrom ?? []).map(id => placeName(bundle, id))];
  return {
    key: `prose:${item.placeId}:${index}`,
    text: item.text,
    places: [...new Set(from)].join(', '),
    source: '',
    struck: false,
    trimmed: Boolean(item.trimmed),
  };
}

/**
 * The Instructions group. Visible knows lines are listed one each, nearest place first, with the place and the
 * learned or replaced caption. Joined instruction prose is listed only for a place that has no visible line, so a
 * legacy paragraph is not dropped and a line is not shown twice as a paragraph. A replacement older than the strike
 * window is absent, the same rule as Home.
 */
export function instructionRows(bundle: UsingBundle, now: Date = new Date()): InstructionRow[] {
  const visible = knowsList((bundle.knows ?? []).map(asKnows), now).all.filter(row => row.line.text.trim() !== '');
  const byPlace = new Map<string, typeof visible>();
  for (const row of visible) {
    const list = byPlace.get(row.line.placeId) ?? [];
    list.push(row);
    byPlace.set(row.line.placeId, list);
  }
  const rows: InstructionRow[] = [];
  const seen = new Set<string>();
  const emit = (placeId: string) => {
    for (const row of byPlace.get(placeId) ?? []) {
      rows.push({
        key: row.line.id,
        text: row.line.text,
        places: placeName(bundle, placeId),
        source: knowsCaption(row.line, row.source, row.struck),
        struck: row.struck,
        trimmed: false,
      });
    }
  };
  for (const place of bundle.places) {
    seen.add(place.id);
    emit(place.id);
  }
  for (const placeId of byPlace.keys()) {
    if (!seen.has(placeId)) emit(placeId);
  }
  bundle.instructions.forEach((item, index) => {
    const credited = [item.placeId, ...(item.alsoFrom ?? [])];
    if (credited.length > 0 && credited.every(id => byPlace.has(id))) return;
    rows.push(proseRow(item, bundle, index));
  });
  return rows;
}

export const settingWords: Record<SettingState, string> = {
  applied: 'In use',
  pending: 'From your next message',
  needsPick: 'Pick one',
  needsYou: 'Waiting for you',
  yours: 'Your choice',
  notNew: 'Not changed',
  unavailable: 'Not applied',
};

export const settingFor = (view: UsingView, field: PolicyField): PlaceSetting | undefined => view.settings.find(setting => setting.field === field);
export const decisionFor = (bundle: UsingBundle, field: PolicyField): PolicyDecision | undefined => bundle.policy.find(decision => decision.field === field);

/** States whose value could be taken by the person, and which of them hold a wider setting that needs a deliberate second step. */
export function applyOffer(setting: PlaceSetting | undefined): { confirm: boolean } | undefined {
  if (!setting || !setting.value) return undefined;
  if (setting.state === 'needsYou') return { confirm: setting.field === 'permissions' };
  if (setting.state === 'notNew') return { confirm: false };
  return undefined;
}

/** Whether the person is asked to choose between places, or may change an earlier choice. */
export const isConflict = (decision: PolicyDecision): boolean => decision.outcome === 'needsPick' || decision.outcome === 'chosen';

/** Which place wins is told in the places' own words: "Release wanted Flash · codeaf decided". */
export function wantedLine(decision: PolicyDecision, bundle: UsingBundle): string | undefined {
  if (decision.wanted.length === 0) return undefined;
  const values = new Set(decision.wanted.map(want => want.value));
  if (decision.outcome === 'agreed' && values.size === 1) return undefined;
  // Once a value is decided, only the wishes that differed explain the conflict.
  const differing = decision.outcome === 'needsPick' ? decision.wanted : decision.wanted.filter(want => want.value !== decision.value);
  if (differing.length === 0) return undefined;
  const wants = differing.map((want: Want) => `${placeName(bundle, want.placeId)} wanted ${valueLabel(decision.field, want.value)}`);
  if (decision.outcome === 'needsPick') return wants.join(' · ');
  const by = decision.decidedBy ? ` · ${placeName(bundle, decision.decidedBy)} ${decision.outcome === 'chosen' ? 'picked' : 'decided'}` : '';
  return `${wants.join(' · ')}${by}`;
}

// A membership change is on screen twice if both copies are drawn: the ring
// event arrives the moment the place changes, and the conversation's own
// record of that change arrives later, often on the next turn. They share the
// undo receipt of the write, so the recorded line replaces the live one.

import type { TurnItem, TurnV2 } from './types';

export type LivePlaceChange = { text: string; undo?: string; placeName?: string };

type ChangeBody = { placeName?: string; sources?: string[]; added?: boolean };

/** The sentence the header's transcript draws. The event names the place; sources ride beside it. */
export function placeChangeLine(text: string, change: ChangeBody): string {
  const name = change.placeName?.trim() ?? '';
  const sources = (change.sources ?? []).map((source) => source.trim()).filter((source) => source.length > 0);
  const spoken = text.trim();
  if (change.added === false) return spoken || (name ? `No longer using ${name}` : '');
  const head = spoken || (name ? `Now also using ${name}` : '');
  if (!head || sources.length === 0 || head.includes(': ')) return head;
  return `${head}: ${sources.join(', ')}`;
}

/** One live membership line, or undefined when the event is not one or says nothing. */
export function livePlaceChange(event: { kind: string; text?: string; placeChange?: unknown }): LivePlaceChange | undefined {
  if (event.kind !== 'placeChange' || !event.placeChange || typeof event.placeChange !== 'object') return undefined;
  const body = event.placeChange as Record<string, unknown>;
  if (typeof body.added !== 'boolean') return undefined;
  const placeName = typeof body.placeName === 'string' ? body.placeName : '';
  const sources = Array.isArray(body.sources) ? body.sources.filter((item): item is string => typeof item === 'string') : [];
  const text = placeChangeLine(event.text ?? '', { placeName, sources, added: body.added });
  if (!text) return undefined;
  const undo = typeof body.undo === 'string' && body.undo.length > 0 ? body.undo : undefined;
  return { text, undo, placeName: placeName.trim() || undefined };
}

/** Keep the first copy of a live line. A later event with the same receipt is the same change. */
export function appendPlaceChange(list: readonly LivePlaceChange[], note: LivePlaceChange): LivePlaceChange[] {
  if (note.undo && list.some((item) => item.undo === note.undo)) return list as LivePlaceChange[];
  if (list.some((item) => item.text === note.text)) return list as LivePlaceChange[];
  return [...list, note];
}

type Held = { turns: TurnV2[]; preface: TurnItem[] };

function recorded(model: Held): { tokens: Set<string>; texts: Set<string> } {
  const tokens = new Set<string>();
  const texts = new Set<string>();
  const take = (text: string, receipts?: readonly string[]) => {
    if (text) texts.add(text);
    receipts?.forEach((id) => { if (id) tokens.add(id); });
  };
  for (const item of model.preface) if (item.kind === 'placeChange') take(item.text, item.undoReceipts);
  for (const turn of model.turns) for (const block of turn.blocks) if (block.kind === 'placeChange') take(block.text, block.undoReceipts);
  return { tokens, texts };
}

/**
 * Live lines the record does not already hold. They sit at the end of the
 * transcript: on the latest turn, or in the preface when the chat has no turn
 * yet. A recorded line with the same undo receipt wins, in the place the record put it.
 */
export function attachLivePlaceChanges<T extends Held>(model: T, live: readonly LivePlaceChange[]): T {
  const { tokens, texts } = recorded(model);
  const fresh: LivePlaceChange[] = [];
  for (const note of live) {
    if (!note.text) continue;
    if (note.undo && tokens.has(note.undo)) continue;
    if (texts.has(note.text)) continue;
    fresh.push(note);
    if (note.undo) tokens.add(note.undo);
    texts.add(note.text);
  }
  if (fresh.length === 0) return model;
  const blocks = fresh.map((note, index) => ({
    kind: 'placeChange' as const,
    id: `place-change:${note.undo ?? index}`,
    text: note.text,
    ...(note.undo ? { undoReceipts: [note.undo] } : {}),
    ...(note.placeName ? { placeName: note.placeName } : {}),
  }));
  if (model.turns.length === 0) return { ...model, preface: [...model.preface, ...blocks] };
  const last = model.turns.length - 1;
  const turns = model.turns.map((turn, index) => (index === last ? { ...turn, blocks: [...turn.blocks, ...blocks] } : turn));
  return { ...model, turns };
}

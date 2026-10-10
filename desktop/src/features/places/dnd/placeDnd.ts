/** Places 8g makes filing additive unless Option is held. This model returns intent;
 * the owner performs the engine write and announces its eventual receipt or refusal. */
export const placeDndTypes = {
  place: 'application/x-codeaf-place',
  chat: 'application/x-codeaf-chat',
  tab: 'application/codeaf-tab',
  files: 'Files',
  urls: 'text/uri-list',
} as const;

export type PlaceDrag =
  | { kind: 'place' | 'chat'; ids: readonly string[] }
  | { kind: 'tab'; id: string }
  | { kind: 'files'; files: readonly File[] }
  | { kind: 'urls'; urls: readonly string[] };
export type PlaceDropAction =
  | { kind: 'chat-add' | 'chat-move'; chatIds: readonly string[]; targetPlaceId: string; fromPlaceId?: string }
  | { kind: 'place-add-parent' | 'place-move'; placeIds: readonly string[]; targetPlaceId: string; fromPlaceId?: string }
  | { kind: 'sources-add'; targetPlaceId: string; sources: Extract<PlaceDrag, { kind: 'files' | 'urls' }> };
export type PlaceDropResult =
  | { status: 'action'; action: PlaceDropAction; dropEffect: 'copy' | 'move' }
  | { status: 'refused'; reason: 'invalid' | 'cycle' | 'missing-source' | 'not-chat' | 'missing-place'; announcement: string };
export type PlaceDropContext = {
  targetPlaceId: string;
  altKey: boolean;
  fromPlaceId?: string;
  places: readonly { id: string; parents: readonly string[] }[];
  /** A tab id is a view, never a chat id. Non-chat and unsent tabs resolve to nothing. */
  chatForTab?: (tabId: string) => string | undefined;
};
export type PlaceTransfer = {
  types: readonly string[];
  getData: (type: string) => string;
  files?: ArrayLike<File>;
};

/** Drag-over can inspect types but cannot read protected payloads. */
export function acceptsPlaceDrag(types: readonly string[]): boolean {
  return Object.values(placeDndTypes).some(type => types.includes(type));
}

const validId = (id: unknown): id is string => typeof id === 'string' && id.trim().length > 0;
const validUrl = (url: string): boolean => {
  try { return ['http:', 'https:'].includes(new URL(url).protocol); }
  catch { return false; }
};

/** Internal payloads take precedence over external fallbacks. A malformed internal
 * drag must not become a link or file drop with a different meaning. */
export function readPlaceDrag(transfer: PlaceTransfer): PlaceDrag | undefined {
  for (const kind of ['place', 'chat'] as const) {
    const type = placeDndTypes[kind];
    if (!transfer.types.includes(type)) continue;
    try {
      const ids: unknown = JSON.parse(transfer.getData(type));
      if (Array.isArray(ids) && ids.length && ids.every(validId)) return { kind, ids: [...new Set(ids)] };
    } catch { /* Foreign payloads are ignored because their identities cannot be trusted. */ }
    return undefined;
  }
  if (transfer.types.includes(placeDndTypes.tab)) {
    const id = transfer.getData(placeDndTypes.tab);
    return validId(id) ? { kind: 'tab', id } : undefined;
  }
  if (transfer.types.includes(placeDndTypes.files)) {
    const files = Array.from(transfer.files ?? []);
    return files.length ? { kind: 'files', files } : undefined;
  }
  if (transfer.types.includes(placeDndTypes.urls)) {
    const urls = transfer.getData(placeDndTypes.urls).split(/\r?\n/).map(line => line.trim()).filter(line => line && !line.startsWith('#'));
    if (!urls.length) return undefined;
    return urls.every(validUrl) ? { kind: 'urls', urls: [...new Set(urls)] } : undefined;
  }
  return undefined;
}

const refuse = (reason: Extract<PlaceDropResult, { status: 'refused' }>['reason'], announcement: string): PlaceDropResult => ({ status: 'refused', reason, announcement });

/** Following parents from the destination finds every ancestor, including through
 * a second parent. Adding an ancestor beneath its descendant would close a loop. */
export function wouldCreatePlaceCycle(placeIds: readonly string[], targetPlaceId: string, places: PlaceDropContext['places']): boolean {
  const parents = new Map(places.map(place => [place.id, place.parents]));
  const forbidden = new Set(placeIds);
  const seen = new Set<string>();
  const pending = [targetPlaceId];
  while (pending.length) {
    const id = pending.pop()!;
    if (forbidden.has(id)) return true;
    if (seen.has(id)) continue;
    seen.add(id);
    pending.push(...(parents.get(id) ?? []));
  }
  return false;
}

/** The engine remains authoritative about graph changes after this snapshot. A
 * move removes only the parent or membership the person dragged from, never all. */
export function resolvePlaceDrop(payload: PlaceDrag | undefined, context: PlaceDropContext): PlaceDropResult {
  if (!payload) return refuse('invalid', 'That item cannot be added to a place.');
  const { targetPlaceId, altKey, fromPlaceId, places } = context;
  const known = new Set(places.map(place => place.id));
  if (!known.has(targetPlaceId)) return refuse('missing-place', 'That place is no longer available.');
  if (payload.kind === 'files' || payload.kind === 'urls') {
    if (payload.kind === 'files' ? !payload.files.length : !payload.urls.length || !payload.urls.every(validUrl)) {
      return refuse('invalid', 'That item cannot be added to a place.');
    }
    return { status: 'action', dropEffect: 'copy', action: { kind: 'sources-add', targetPlaceId, sources: payload } };
  }
  if (payload.kind === 'tab') {
    const chat = context.chatForTab?.(payload.id);
    if (!validId(chat)) return refuse('not-chat', 'Only a saved chat can be added to a place.');
    return resolvePlaceDrop({ kind: 'chat', ids: [chat] }, context);
  }
  if (!payload.ids.length || !payload.ids.every(validId)) return refuse('invalid', 'That item cannot be added to a place.');
  if (payload.kind === 'place') {
    if (payload.ids.some(id => !known.has(id))) return refuse('missing-place', 'That place is no longer available.');
    if (wouldCreatePlaceCycle(payload.ids, targetPlaceId, places)) return refuse('cycle', 'A place cannot be put inside itself or one of its children. Nothing was changed.');
  }
  if (altKey && (!fromPlaceId || !known.has(fromPlaceId) || fromPlaceId === targetPlaceId)) {
    return refuse('missing-source', 'Choose a different place to move this from. Nothing was changed.');
  }
  if (altKey && payload.kind === 'place' && payload.ids.some(id => !places.find(place => place.id === id)?.parents.includes(fromPlaceId!))) {
    return refuse('missing-source', 'This place is no longer inside the place it was dragged from. Nothing was changed.');
  }
  const source = altKey ? { fromPlaceId } : {};
  const action: PlaceDropAction = payload.kind === 'chat'
    ? { kind: altKey ? 'chat-move' : 'chat-add', chatIds: [...new Set(payload.ids)], targetPlaceId, ...source }
    : { kind: altKey ? 'place-move' : 'place-add-parent', placeIds: [...new Set(payload.ids)], targetPlaceId, ...source };
  return { status: 'action', action, dropEffect: altKey ? 'move' : 'copy' };
}

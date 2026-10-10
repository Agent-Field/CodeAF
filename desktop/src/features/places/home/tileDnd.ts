// Tile drag and drop (Places 8g "Organizing", Components "Place tile" drop target).
// A plain drop adds. Option (Alt) moves. The tile says "Add here" either way: that is the only
// label the component draws, and Option changes the drop, not the words (TILE-DND-1).
// The keyboard way in is the place menu's "Add to another place…" and a chat row's "Add to a place…".
// Those rows already live on the menus; this module only decides when a tile is a target.

/** The words on a tile that can take the drag. Components draws nothing else. */
export const tileDropLabel = 'Add here';

export type TileDragKind = 'chat' | 'place';
export type TileDragPayload = { kind: TileDragKind; ids: readonly string[] };
export type TileDropMode = 'add' | 'move';

export type TileDropGate = {
  targetId: string;
  /** Archived tiles are not places you can file into. */
  archived?: boolean;
  /** Offline: the page keeps its last picture and accepts no write. */
  readOnly?: boolean;
  /** False when the owner never wired filing, so the tile grows no drop affordance. */
  canFile: boolean;
  /** Known while the drag started in this window. The browser hides it from other windows until the drop. */
  payload?: TileDragPayload;
  /** MIME types are readable during the drag even when the payload is not. */
  types?: ArrayLike<string>;
};

/** Same type names place-actions writes, so a chat row and a tile agree on what is in flight. */
const chatType = 'application/x-codeaf-chat';
const placeType = 'application/x-codeaf-place';

/**
 * Option moves instead of adds (Places 8g). A plain drop adds a parent or a membership and leaves
 * the others. Option makes a place's only parent the tile, and takes a chat out of the place it
 * was dragged from. Interactions describes the place gesture the other way around; Places 8g wins.
 */
export const tileDropMode = (event: { altKey: boolean }): TileDropMode => (event.altKey ? 'move' : 'add');

/** The cursor: copy while the drop adds, move while Option is held. The label does not follow it. */
export const tileDropEffect = (event: { altKey: boolean }): 'copy' | 'move' => (tileDropMode(event) === 'move' ? 'move' : 'copy');

function listed(types: ArrayLike<string> | undefined): string[] {
  return types ? Array.from(types) : [];
}

function payloadCanLand(payload: TileDragPayload, targetId: string): boolean {
  if (!payload.ids.length || payload.ids.some(id => id.trim() === '')) return false;
  // A place cannot be dropped on itself. A cycle through a child is the engine's refusal, shown as a sentence.
  return !(payload.kind === 'place' && payload.ids.includes(targetId));
}

/**
 * Whether the tile draws "Add here". Option is not an input: the words stay put while it is held.
 * A drag from this window is judged by its payload. A drag whose payload the browser will not
 * reveal yet is judged by its type, and the drop checks the ids again before anything is written.
 */
export function tileDropVisible(gate: TileDropGate): boolean {
  if (gate.readOnly || gate.archived || !gate.canFile) return false;
  if (gate.payload) return payloadCanLand(gate.payload, gate.targetId);
  const types = listed(gate.types);
  return types.includes(chatType) || types.includes(placeType);
}

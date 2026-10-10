/** The JSON contracts sent by the canonical Go places bridge. */
export type Tint = 'tide' | 'iris' | 'rose' | 'sand' | 'sage' | 'graphite';
export type AddedBy = 'you' | 'ai';
export type SourceKind = 'folder' | 'repo' | 'file' | 'url' | 'chat';

/** What a place says about the work in it; counts of chats the engine could read. */
export type StatusRollup = { chats: number; running: number; needsYou: number; incomplete: number; failedTasks: number };
export type PlaceCounts = { children: number; descendants: number; chats: number; chatsInclusive: number };
export type PlaceRef = { id: string; name: string };

export type PlaceView = {
  id: string; name: string; parents: string[];
  /** The place's own choice; '' when it inherits. */
  tint: Tint | '';
  effectiveTint: Tint;
  archived: boolean; pinned: boolean;
  createdAt?: string; lastOpenedAt?: string; archivedAt?: string;
  counts: PlaceCounts;
  /** Chats filed directly here. */
  status: StatusRollup;
  /** This place and everything below it, each chat once. */
  statusInclusive: StatusRollup;
  hasInstructions: boolean; sourceCount: number;
  /** Parents after the first: "Release · also in Software". */
  alsoIn: PlaceRef[];
};

export type SourceCheck = { state: 'ok' | 'missing' | 'unreadable' | 'unknown'; note?: string; title?: string };
export type SourceView = { id: string; kind: SourceKind; ref: string; label?: string; addedBy: AddedBy; at?: string; check: SourceCheck };
export type PlacePolicy = { model?: string; permissions?: string };
export type PlaceDetail = PlaceView & { instructions: string; sources: SourceView[]; policy: PlacePolicy };

export type RailView = { pinned: PlaceView[]; open: PlaceView[]; openWindowHours: number };
export type NowView = { chats: number; status: StatusRollup };
export type PlacesTotals = { places: number; placed: number; unplaced: number; running: number; needsYou: number; missingChats: number };
export type PlacesRecovery = { kind: 'quarantined' | 'repaired'; reason: string; movedTo: string; repairs?: string[]; at: string };

export type PlacesGraph = {
  revision: number; generation: number; nodes: PlaceNode[]; unplaced: string[]; places: PlaceView[]; rail: RailView; now: NowView; totals: PlacesTotals; readAt: string;
  /** Set when the store found a damaged file and kept a copy aside. */
  recovery?: PlacesRecovery;
};
export type PlacesStatus = {
  revision: number; readAt: string;
  places: Record<string, { status: StatusRollup; statusInclusive: StatusRollup }>;
  now: NowView; totals: PlacesTotals;
};

export type ChatPlace = { id: string; name: string; tint: Tint; addedBy: AddedBy };
export type ChatRow = {
  id: string; title: string; project: string; workspace: string;
  /** The conversation's journal: the path a window reattaches with. Absent when the engine could not say. */
  sessionFile?: string;
  at?: string; created?: string; model?: string;
  archived: boolean; live: boolean; doing: string; needsYou: boolean; reason?: string;
  tasks: { running: number; incomplete: number; done: number; failed: number };
  places: ChatPlace[];
  /** Who filed it in the place this Home is about. */
  addedBy?: AddedBy;
};
export type Attention = {
  kind: 'needsYou' | 'running'; chatId: string; chatTitle: string; placeId: string; placeName: string;
  text: string; taskId?: string; since?: string;
};
/** One conversation the "Since yesterday" roll-up quotes. */
export type RecapItem = {
  chatId: string; chatTitle: string; placeId?: string; placeName?: string; at: string;
  line: string; outcome?: string; attention?: 'needsYou' | 'running';
};
/** The engine's roll-up of what this Home's conversations wrote about themselves in the last day. Absent when there is no evidence. */
export type HomeRecap = {
  label: string; text: string; since: string; chats: number; items: RecapItem[];
  /** Waiting or running chats with no readable recap; the text says nothing about them. */
  unsummarised: number;
};
export type Crumb = { id: string; name: string; tint: Tint };
export type HomeDigest = {
  kind: 'place' | 'root' | 'now'; title: string;
  /** Present only for a real place. */
  place?: PlaceDetail;
  breadcrumb: Crumb[]; children: PlaceView[]; chats: ChatRow[]; chatsTruncated: boolean;
  attention: Attention[]; status: StatusRollup; counts: PlaceCounts; missingChats: number;
  /** "Since yesterday". Absent unless a recent conversation wrote a recap. */
  recap?: HomeRecap;
  /** What a place carries into a chat, in counts ("Instructions · 3 sources · 1 missing"). Absent when nothing, and for root and Now. */
  contextLine?: string;
  revision: number; readAt: string;
};

export type Receipt = { id: string; action: string; subject?: string; beforeRevision: number; afterRevision: number; at: string };
export type Membership = { chatId: string; placeId: string; addedBy: AddedBy; at?: string };
/** The receipt of one write. 2xx means the store committed. */
export type Mutation = {
  revision: number; generation?: number; receipt?: Receipt; rail?: Rail; chats?: number; children?: number; receipts: Receipt[]; noop: boolean; undo: string[];
  place?: PlaceDetail; result?: Record<string, unknown>; memberships?: Membership[];
};
export type DeleteImpact = { children: number; chatsHere: number; wouldBeUnplaced: string[] };
export type ChatPlacesAnswer = {
  chatId: string; known: boolean; workspace?: string; sessionFile?: string;
  places: { id: string; name: string; tint: Tint; addedBy: AddedBy; at?: string; archived: boolean }[];
};
export type UndoAnswer = { revision: number; undone: number };
/** A place named by the model rule: who decided, or who wanted what. */
export type EffectiveModelPlace = { id: string; name: string; model?: string };
/**
 * Which model the first message typed on a place's Home would run on, as the engine's own place rule decides it.
 * `applies` overrides the Conversation role; `needsPick` applies nothing (the role's model runs and the chat asks);
 * `none` and `unavailable` leave the role's model in charge.
 */
export type EffectiveModel = {
  placeId: string; revision: number;
  state: 'none' | 'applies' | 'needsPick' | 'unavailable';
  model?: string; decidedBy?: EffectiveModelPlace; outcome?: 'agreed' | 'decided';
  wanted?: EffectiveModelPlace[]; reason?: string;
};

export type CreatePlaceAsk = { name: string; parent?: string; parents?: string[]; tint?: Tint; instructions?: string; ifGeneration?: number; ifRevision?: number };
export type UpdatePlaceAsk = { name?: string; tint?: Tint | ''; instructions?: string; policy?: PlacePolicy; ifGeneration?: number; ifRevision?: number };
export type ParentsAsk = ({ add: string } | { remove: string } | { set: string[] }) & { ifGeneration?: number; ifRevision?: number };


export type { UsingBundle as Bundle, UsingView, PolicyField } from './using-client.ts';
export type { EngineSnapshot as PlaceSession } from '../chat/engine-client.ts';
export type PlaceNode = PlaceView;
export type Rail = RailView;
export type HomeView = {
  place?: PlaceDetail; breadcrumb: Crumb[]; since?: { text: string; anchor: string };
  attention: { chatId: string; title: string; originPlaceId?: string; state: string; startedAt?: string }[];
  children: { id: string; name: string; effectiveTint: Tint; needsYou?: number; failed?: number; chats?: number; childPlaces?: number; alsoIn?: string[] }[];
  chats?: HomeChat[]; unplaced?: HomeChat[];
};
export type HomeChat = { chatId: string; sessionFile?: string; title: string; digest?: string; state?: string; tasksRunning?: number; at?: string };
export type PlacesRecord = { generation: number; nodes: PlaceNode[]; rail: Rail; members?: Membership[] };
export type PlacesRecordState = { generation: number; nodes: PlaceNode[]; rail: Rail; members?: Membership[] };
export type PlaceChangeEvent = { placeId: string; placeName: string; tint: string; sources: string[]; added: boolean; undo: string; at: string };
export type ChatSource = { path: string; arrival: 'said' | 'kept'; referred?: string; mode?: string; chose?: string; repository?: boolean };

/** Soft rail changes keep the generation, so only an identical replay is ignored. */
export function applyPlacesRecord<T extends PlacesRecordState>(state: T, record: PlacesRecord): T {
  if (record.generation < state.generation) return state;
  if (record.generation === state.generation && JSON.stringify(record.nodes) === JSON.stringify(state.nodes)
    && JSON.stringify(record.rail) === JSON.stringify(state.rail)
    && (record.members === undefined || JSON.stringify(record.members) === JSON.stringify(state.members))) return state;
  return { ...state, generation: record.generation, nodes: record.nodes, rail: record.rail,
    ...(record.members === undefined ? {} : { members: record.members }) };
}

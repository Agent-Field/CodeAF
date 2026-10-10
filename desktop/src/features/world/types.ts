import type { Membership, PlaceView, RailView } from '../places/client.ts';

/** These fields mirror WorldChatRow, InboxItem and jobWire in desktopbridge. */
export type WorldRow = {
 chatId: string; sessionFile?: string; title?: string; workspace?: string;
 running: boolean; needsYou: number; failed: number; tasksRunning: number;
 tasksTotal: number; updatedAt?: string; attached: boolean; archived: boolean;
};
export type AttentionItem = {
 id: string; chatId: string; sessionFile?: string; sessionId?: string;
 title?: string; kind: string; head?: string; at?: string;
 /** Absent on older engines, whose attention questions all block a conversation. */
 blocking?: boolean;
};
export type EngineJob = {
 id: number; name?: string; command?: string; detail?: string; kind?: string;
 state?: string; startedAt?: string; elapsedMs?: number; exitCode?: number;
 ticks?: number; logPath?: string;
};
export type JobsRollup = { chatId: string; running: number; jobs: EngineJob[] };
export type PlacesRecord = { generation: number; nodes: PlaceView[]; rail: RailView; members?: Membership[] };
/** Workspace notifications invalidate the saved document; they are not that document. */
export type WorkspaceRecord = { key: string; revision: number; writer?: string };
export type WorldFull = {
 rows: WorldRow[]; items: AttentionItem[];
 jobs?: JobsRollup[]; places?: PlacesRecord; workspaces?: WorkspaceRecord[];
};
type Envelope<T extends string, P> = { epoch: string; seq: number; type: T; at: string; payload: P };
export type WorldRecord =
 | Envelope<'reset', WorldFull>
 | Envelope<'world', { rows: WorldRow[]; removed: string[] }>
 | Envelope<'attention', { items: AttentionItem[] }>
 | Envelope<'jobs', JobsRollup>
 | Envelope<'places', PlacesRecord>
 | Envelope<'workspace', WorkspaceRecord>;

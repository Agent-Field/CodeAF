// The wire shapes of the engine's history routes (internal/desktopbridge/history.go). Plain data, no React.

export type HistoryFilter = 'all' | 'decisions' | 'files' | 'tasks' | 'open';
export const historyFilters: readonly HistoryFilter[] = ['all', 'decisions', 'files', 'tasks', 'open'];
export const filterLabels: Record<HistoryFilter, string> = { all: 'All', decisions: 'Decisions', files: 'Files', tasks: 'Tasks', open: 'Open' };

/** One changed file with its line counts. */
export type HistoryFile = { path: string; added: number; removed: number };

/** What a conversation is doing now, in the engine's words. Only an open conversation has any state but idle. */
export type HistoryState = 'idle' | 'working' | 'needs-you';

/** One conversation as a history row. A task is never a row of its own. */
export type HistoryItem = {
  id: string;
  /** The transcript path: what POST /sessions takes to attach the conversation. */
  sessionFile: string;
  title: string;
  /** The recap's one sentence of substance. Absent until a recap has been written. */
  line?: string;
  /** RFC 3339: when the person last spoke here. */
  at: string;
  messages: number;
  tasks: number;
  tasksRunning: number;
  files: HistoryFile[];
  fileCount: number;
  decisions: number;
  state: HistoryState;
  /** The question a needs-you conversation waits on. */
  reason?: string;
  open: boolean;
  archived: boolean;
  workspace?: string;
};

export type HistoryPage = { total: number; matching: number; items: HistoryItem[]; next?: string };

/** One decision in a recap: who made it ("you" or "codeaf") and how ("accepted"). */
export type Decision = { text: string; by: string; how?: string };
export type Recap = { line: string; discussed: string; decided: Decision[]; outcome: string; files: HistoryFile[]; updatedAt?: string; messages?: number };
export type HistoryDetail = { item: HistoryItem; recap?: Recap; stale?: boolean };

export type HistoryMessage = { index: number; role: 'user' | 'assistant'; text: string; at?: string };
export type HistoryMessages = { total: number; messages: HistoryMessage[] };

export type BestMatch = { item: HistoryItem; answer: string; messageIndex?: number; terms: string[] };
export type DecisionHit = { id: string; title: string; context: string; at: string };
export type DiscussHit = { id: string; title: string; snippet: string; messageIndex?: number; at: string };
export type FileHit = { path: string; conversations: number; last: string; ids?: string[] };
export type TaskHit = { id: string; taskId: string; conversationTitle: string; title: string; snippet: string; at: string };
export type SearchCounts = { decisions: number; discussed: number; files: number; tasks: number };
export type SearchResult = {
  query: string; best?: BestMatch; decisions: DecisionHit[]; discussed: DiscussHit[]; files: FileHit[]; tasks: TaskHit[]; counts: SearchCounts;
};

/** The archive route's answer. */
export type ArchiveResult = { changed: number };

/** The delete route's answer. `undoToken` is absent when nothing moved. */
export type DeleteResult = { deleted: number; undoToken?: string };

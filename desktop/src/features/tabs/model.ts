export type Tab = { id: string; title: string; pinned: boolean; groupId?: string; draft: string };
export type TabGroup = { id: string; title: string; collapsed: boolean };
export type WorkspaceState = { tabs: Tab[]; groups: TabGroup[]; activeId: string; closed: Tab[]; nextNumber: number; recentIds: string[] };
export const storageKey = 'codeaf.desktop.workspace.v1';
const createId = () => crypto.randomUUID();
export function initialWorkspace(): WorkspaceState {
 const id = createId();
 return { tabs: [{ id, title: 'New conversation', pinned: false, draft: '' }], groups: [], activeId: id, closed: [], nextNumber: 2, recentIds: [id] };
}
const isTab = (value: unknown): value is Tab => {
 if (!value || typeof value !== 'object') return false;
 const tab = value as Partial<Tab>;
 return typeof tab.id === 'string' && !!tab.id && typeof tab.title === 'string' && typeof tab.draft === 'string' && typeof tab.pinned === 'boolean' && (tab.groupId === undefined || typeof tab.groupId === 'string');
};
const isGroup = (value: unknown): value is TabGroup => {
 if (!value || typeof value !== 'object') return false;
 const group = value as Partial<TabGroup>;
 return typeof group.id === 'string' && !!group.id && typeof group.title === 'string' && typeof group.collapsed === 'boolean';
};
export function readWorkspace(): WorkspaceState {
 try {
  const saved = JSON.parse(localStorage.getItem(storageKey) ?? 'null') as Partial<WorkspaceState> | null;
  if (!saved || !Array.isArray(saved.tabs) || !saved.tabs.length || !Array.isArray(saved.groups) || !Array.isArray(saved.closed)) return initialWorkspace();
  if (!saved.tabs.every(isTab) || !saved.groups.every(isGroup) || !saved.closed.every(isTab)) return initialWorkspace();
  if (new Set(saved.tabs.map(t => t.id)).size !== saved.tabs.length || new Set(saved.groups.map(g => g.id)).size !== saved.groups.length || new Set(saved.closed.map(t => t.id)).size !== saved.closed.length || saved.closed.some(t => saved.tabs?.some(open => open.id === t.id))) return initialWorkspace();
  const groupIds = new Set(saved.groups.map(g => g.id));
  const tabs = saved.tabs.map(tab => ({ ...tab, groupId: !tab.pinned && tab.groupId && groupIds.has(tab.groupId) ? tab.groupId : undefined }));
  const activeId = tabs.some(tab => tab.id === saved.activeId) ? saved.activeId! : tabs[0].id;
  return {
   tabs, groups: saved.groups.filter(g => tabs.some(t => t.groupId === g.id)), closed: saved.closed.slice(-20),
   recentIds: [...new Set([activeId, ...(Array.isArray(saved.recentIds) ? saved.recentIds.filter(id => typeof id === 'string' && tabs.some(t => t.id === id)) : []), ...tabs.map(t => t.id)])],
   activeId,
   nextNumber: Number.isSafeInteger(saved.nextNumber) && saved.nextNumber! > 0 && saved.nextNumber! < 1000000 ? saved.nextNumber! : tabs.length + 1,
  };
 } catch { return initialWorkspace(); }
}
function normalize(state: WorkspaceState): WorkspaceState {
 return { ...state, recentIds: [...new Set([state.activeId, ...state.recentIds.filter(id => state.tabs.some(t => t.id === id)), ...state.tabs.map(t => t.id)])], groups: state.groups.filter(g => state.tabs.some(t => t.groupId === g.id)) };
}
export type WorkspaceAction =
 | { type: 'new'; groupId?: string }
 | { type: 'select'; id: string }
 | { type: 'close'; id: string }
 | { type: 'reopen' }
 | { type: 'pin'; id: string }
 | { type: 'rename'; id: string; title: string }
 | { type: 'draft'; id: string; draft: string }
 | { type: 'group'; id: string }
 | { type: 'move-group'; id: string; groupId?: string }
 | { type: 'rename-group'; id: string; title: string }
 | { type: 'collapse-group'; id: string }
 | { type: 'ungroup'; id: string }
 | { type: 'reorder'; id: string; targetId: string };
export function workspaceReducer(state: WorkspaceState, action: WorkspaceAction): WorkspaceState {
 switch (action.type) {
  case 'new': {
   const tab: Tab = { id: createId(), title: `New conversation ${state.nextNumber}`, draft: '', pinned: false, groupId: action.groupId };
   return { ...state, tabs: [...state.tabs, tab], activeId: tab.id, recentIds: [tab.id, ...state.recentIds], nextNumber: state.nextNumber + 1, groups: state.groups.map(g => g.id === action.groupId ? { ...g, collapsed: false } : g) };
  }
  case 'select': return { ...state, activeId: action.id, recentIds: [action.id, ...state.recentIds.filter(id => id !== action.id)], groups: state.groups.map(g => state.tabs.find(t => t.id === action.id)?.groupId === g.id ? { ...g, collapsed: false } : g) };
  case 'close': {
   const closing = state.tabs.find(t => t.id === action.id);
   if (!closing) return state;
   const order = [...state.tabs.filter(t => t.pinned), ...state.tabs.filter(t => !t.pinned && !t.groupId), ...state.groups.flatMap(g => state.tabs.filter(t => t.groupId === g.id && (!g.collapsed || t.id === state.activeId)))];
   const index = order.findIndex(t => t.id === closing.id);
   const remaining = order.filter(t => t.id !== closing.id);
   const tabs = state.tabs.filter(t => t.id !== action.id);
   const closed = [...state.closed.slice(-19), closing];
   if (!tabs.length) return { ...initialWorkspace(), closed, nextNumber: state.nextNumber };
   return normalize({ ...state, tabs, closed, activeId: state.activeId === action.id ? (remaining[Math.min(Math.max(index, 0), remaining.length - 1)] ?? tabs[0]).id : state.activeId, groups: state.groups.filter(g => tabs.some(t => t.groupId === g.id)) });
  }
  case 'reopen': {
   const tab = state.closed[state.closed.length - 1];
   if (!tab) return state;
   const groupId = state.groups.some(g => g.id === tab.groupId) ? tab.groupId : undefined;
   return { ...state, tabs: [...state.tabs, { ...tab, groupId }], closed: state.closed.slice(0, -1), activeId: tab.id, recentIds: [tab.id, ...state.recentIds.filter(id => id !== tab.id)], groups: state.groups.map(g => g.id === groupId ? { ...g, collapsed: false } : g) };
  }
  case 'pin': return normalize({ ...state, tabs: state.tabs.map(t => t.id === action.id ? { ...t, pinned: !t.pinned, groupId: undefined } : t) });
  case 'rename': return { ...state, tabs: state.tabs.map(t => t.id === action.id ? { ...t, title: action.title.trim() || 'New conversation' } : t) };
  case 'draft': return { ...state, tabs: state.tabs.map(t => t.id === action.id ? { ...t, draft: action.draft } : t) };
  case 'group': {
   const group: TabGroup = { id: createId(), title: 'New group', collapsed: false };
   return normalize({ ...state, groups: [...state.groups, group], tabs: state.tabs.map(t => t.id === action.id ? { ...t, pinned: false, groupId: group.id } : t) });
  }
  case 'move-group': return normalize({ ...state, tabs: state.tabs.map(t => t.id === action.id ? { ...t, groupId: action.groupId, pinned: false } : t), groups: state.groups.map(g => g.id === action.groupId ? { ...g, collapsed: false } : g) });
  case 'rename-group': return { ...state, groups: state.groups.map(g => g.id === action.id ? { ...g, title: action.title.trim() || 'New group' } : g) };
  case 'collapse-group': return { ...state, groups: state.groups.map(g => g.id === action.id ? { ...g, collapsed: !g.collapsed } : g) };
  case 'ungroup': return { ...state, tabs: state.tabs.map(t => t.groupId === action.id ? { ...t, groupId: undefined } : t), groups: state.groups.filter(g => g.id !== action.id) };
  case 'reorder': {
   const tab = state.tabs.find(t => t.id === action.id);
   const target = state.tabs.find(t => t.id === action.targetId);
   if (!tab || !target || tab.id === target.id) return state;
   const tabs = state.tabs.filter(t => t.id !== tab.id);
   tabs.splice(tabs.findIndex(t => t.id === target.id), 0, { ...tab, pinned: target.pinned, groupId: target.groupId });
   return normalize({ ...state, tabs });
  }
 }
}

import type { SharedTab, SharedWorkspace, WindowLocal } from './shared.ts';
import { newTab } from '../tabs/helpers.ts';
import type { Pane } from '../tabs/types.ts';
export type SavedChatLocation = { chatId: string; workspace: string; placeIds: readonly string[] };
export type TransferMatch = { placeId: string; sources: readonly { kind: string; ref: string }[]; resolve: (sessionFile: string) => SavedChatLocation | undefined };
const panes = (tab: SharedTab): readonly Pane[] => tab.split?.panes ?? [tab];
const within = (path: string, root: string) => {
  const normalize = (v: string) => v.replace(/\\/g, '/').replace(/\/+$/, '');
  const p = normalize(path), r = normalize(root);
  return !!r && (p === r || p.startsWith(`${r}/`));
};
/** Whole splits move only when every pane belongs. Unknown, pinned and navigation panes stay in Now. */
export function matchingTabs(workspace: SharedWorkspace, match: TransferMatch): string[] {
  return workspace.tabs.filter(tab => !tab.pinned && panes(tab).every(pane => {
    if (pane.kind === 'home' || pane.kind === 'inbox' || !pane.sessionFile) return false;
    const chat = match.resolve(pane.sessionFile);
    return !!chat && (chat.placeIds.includes(match.placeId) || match.sources.some(s => (s.kind === 'folder' || s.kind === 'repo') && within(chat.workspace, s.ref)));
  })).map(tab => tab.id);
}
export type TransferReceipt = { tabIds: string[]; tabs: SharedTab[]; groups: SharedWorkspace['groups']; sourceOrder: string[]; placeholder?: SharedTab };
export type TransferPlan = { source: SharedWorkspace; destination: SharedWorkspace; receipt: TransferReceipt };
const identityIds = (tab: SharedTab) => [tab.id, ...(tab.split?.panes.map(p => p.id) ?? [])];
const canonical = (value: unknown) => JSON.stringify(value, (_key, next) => next && typeof next === 'object' && !Array.isArray(next) ? Object.fromEntries(Object.keys(next).sort().map(key => [key, next[key]])) : next);
const same = (a: unknown, b: unknown) => canonical(a) === canonical(b);
/** Rebase a fixed set of intended ids over the newest pair, without substituting newly opened tabs. */
export function planTransfer(source: SharedWorkspace, destination: SharedWorkspace, ids: readonly string[], emptySource?: SharedTab): TransferPlan {
  const wanted = new Set(ids), moving = source.tabs.filter(t => wanted.has(t.id));
  if (!moving.length || moving.length !== wanted.size) throw new Error('Some matching tabs changed. Review the offer again.');
  if (moving.some(t => t.pinned || panes(t).some(p => p.kind === 'home' || p.kind === 'inbox'))) throw new Error('Navigation and pinned tabs stay here.');
  const remaining = source.tabs.filter(t => !wanted.has(t.id));
  const placeholder = !remaining.length ? emptySource ?? newTab() : undefined;
  if (placeholder) remaining.push(placeholder);
  const existing = new Set([...destination.tabs, ...destination.closed].flatMap(identityIds));
  if (moving.some(t => identityIds(t).some(id => existing.has(id)))) throw new Error('A matching tab already exists in this place.');
  const groupIds = new Set(moving.flatMap(t => t.groupId ? [t.groupId] : []));
  const groups = source.groups.filter(g => groupIds.has(g.id));
  for (const group of groups) {
    const there = destination.groups.find(g => g.id === group.id);
    if (there && !same(there, group)) throw new Error('A tab group changed in the destination. Review the offer again.');
  }
  return {
    source: { ...source, tabs: remaining, groups: source.groups.filter(g => !groupIds.has(g.id) || remaining.some(t => t.groupId === g.id)) },
    destination: { ...destination, tabs: [...destination.tabs, ...moving], groups: [...destination.groups, ...groups.filter(g => !destination.groups.some(e => e.id === g.id))], nextNumber: Math.max(source.nextNumber, destination.nextNumber) },
    receipt: { tabIds: moving.map(t => t.id), tabs: moving, groups, sourceOrder: source.tabs.map(t => t.id), ...(placeholder ? { placeholder } : {}) },
  };
}
/** Undo refuses later edits to moved identities and preserves everything else. */
export function planTransferUndo(source: SharedWorkspace, destination: SharedWorkspace, receipt: TransferReceipt): TransferPlan {
  for (const tab of receipt.tabs) {
    if (!same(destination.tabs.find(t => t.id === tab.id), tab)) throw new Error('A moved tab changed. Undo cannot replace its newer contents.');
  }
  for (const group of receipt.groups) {
    const current = destination.groups.find(g => g.id === group.id);
    if (current && !same(current, group)) throw new Error('A moved tab group changed. Undo cannot replace it.');
  }
  const reverse = planTransfer(destination, source, receipt.tabIds);
  // Insert each returning tab by its old neighbours, keeping later source edits in their current order.
  const tabs = source.tabs.filter(tab => !receipt.placeholder || tab.id !== receipt.placeholder.id || !same(tab, receipt.placeholder));
  for (const id of receipt.sourceOrder) {
    const returned = receipt.tabs.find(t => t.id === id);
    if (!returned) continue;
    const oldIndex = receipt.sourceOrder.indexOf(id);
    const next = receipt.sourceOrder.slice(oldIndex + 1).find(next => tabs.some(t => t.id === next));
    const previous = [...receipt.sourceOrder.slice(0, oldIndex)].reverse().find(previous => tabs.some(t => t.id === previous));
    const at = next ? tabs.findIndex(t => t.id === next) : previous ? tabs.findIndex(t => t.id === previous) + 1 : tabs.length;
    tabs.splice(at, 0, returned);
  }
  reverse.destination = { ...reverse.destination, tabs };
  return reverse;
}
/** Copy only transferred focus/scroll; these never enter a shared engine document. */
export function transferredLocal(previous: WindowLocal, source: WindowLocal, receipt: TransferReceipt): WindowLocal {
  const tabs = new Set(receipt.tabIds), ids = new Set(receipt.tabs.flatMap(identityIds));
  return { ...previous, focus: { ...previous.focus, ...Object.fromEntries(Object.entries(source.focus).filter(([id]) => tabs.has(id))) }, scroll: { ...previous.scroll, ...Object.fromEntries(Object.entries(source.scroll).filter(([id]) => ids.has(id))) } };
}

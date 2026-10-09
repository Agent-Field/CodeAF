// The workspace: owns the reducer state and composes the strip, the content card and the dialogs.
// Everything with a lane of its own lives in a sibling file (see ARCHITECTURE.md).
import { useEffect, useReducer, useRef, useState, type ReactNode } from 'react';
import { Button, Icon, Text, TextInput } from '../../components/ui';
import design from '../../design/tokens.json';
import { summarize, type TabSummary } from '../conversation/tabSummary';
import { useBackgroundSessions } from '../conversation/useBackgroundSessions';
import type { TabsApi } from './context';
import type { PaneActions } from './kinds/slots';
import { kindDef } from './kinds/registry';
import { focusedPane, panesOf, readWorkspace, storageKey, visibleTabs, workspaceReducer, type Pane, type Tab } from './model';
import { PaneGrid } from './PaneGrid';
import { TabOverview } from './TabOverview';
import { TabStrip } from './TabStrip';
import { useDesktopTabActions, useTabKeys, type Switcher } from './useTabKeys';
import './workspace.css';

type Props = { enabled: boolean; onActivate: () => void; leading?: ReactNode };

export function Workspace({ enabled, onActivate, leading }: Props) {
  const [state, dispatch] = useReducer(workspaceReducer, undefined, readWorkspace);
  const [summaries, setSummaries] = useState<Record<string, TabSummary>>({});
  const [now, setNow] = useState(Date.now);
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), design.interaction.activityRefreshInterval); return () => window.clearInterval(timer); }, []);
  function receiveSummary(id: string, summary: TabSummary) {
    setSummaries(current => ({ ...current, [id]: summary }));
    if (summary.title) dispatch({ type: 'title', id, title: summary.title, source: 'engine' });
    else if (summary.firstLine) dispatch({ type: 'title', id, title: summary.firstLine, source: 'message' });
  }
  function openTaskTab(source: Pane, taskId: string, title: string) {
    const tab: Tab = { id: crypto.randomUUID(), kind: 'task', title, titleSource: 'manual', pinned: false, draft: '', sessionFile: source.sessionFile, route: { taskId, back: [''], forward: [] } };
    dispatch({ type: 'open', tab, background: true });
  }
  const [overviewOpen, setOverviewOpen] = useState(false);
  const [switcher, setSwitcher] = useState<Switcher>(null);
  const switcherRef = useRef<Switcher>(null);
  const switcherFocus = useRef<HTMLDivElement>(null);
  const [rename, setRename] = useState<{ id: string; group: boolean; value: string } | null>(null);
  const renameDialog = useRef<HTMLDialogElement>(null);
  const renameInput = useRef<HTMLInputElement>(null);
  const overviewTrigger = useRef<HTMLButtonElement>(null);
  const active = state.tabs.find(tab => tab.id === state.activeId) ?? state.tabs[0];
  const visible = visibleTabs(state);

  // Inactive tabs (every pane of them) keep observing their sessions; the active tab's panes stream themselves.
  useBackgroundSessions(state.tabs.filter(tab => tab.id !== active.id).flatMap(tab => panesOf(tab)).filter(pane => pane.sessionFile).map(pane => ({ id: pane.id, sessionFile: pane.sessionFile! })), (id, snapshot) => receiveSummary(id, summarize(snapshot)));
  useEffect(() => { try { localStorage.setItem(storageKey, JSON.stringify(state)); } catch { /* A full or unavailable store must not interrupt local tab navigation. */ } }, [state]);

  useEffect(() => {
    if (!switcher) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    switcherFocus.current?.focus();
    return () => {
      if (previous?.closest('.workspace-tab')) document.querySelector<HTMLElement>('.workspace-tabstrip [aria-selected="true"]')?.focus();
      else if (previous?.isConnected) previous.focus();
    };
  }, [!!switcher]);
  useEffect(() => {
    if (!switcher) return;
    switcherFocus.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest', behavior: 'instant' });
  }, [switcher]);
  useEffect(() => {
    if (rename) { renameDialog.current?.showModal(); renameInput.current?.focus(); renameInput.current?.select(); }
    else renameDialog.current?.close();
  }, [rename]);

  function closeTab(id: string) {
    const restore = !!document.activeElement?.closest('.workspace-tab');
    dispatch({ type: 'close', id });
    if (restore) requestAnimationFrame(() => document.querySelector<HTMLElement>('.workspace-tabstrip [aria-selected="true"]')?.focus());
  }
  function startRename(id: string, group = false) { setRename({ id, group, value: (group ? state.groups : state.tabs).find(item => item.id === id)?.title ?? '' }); }
  useTabKeys({ enabled, state, dispatch, visible, overviewOpen, setOverviewOpen, closeTab, switcherRef, setSwitcher });
  useDesktopTabActions({ state, dispatch, visible, renaming: !!rename, onActivate, closeTab, setOverviewOpen });

  const api: TabsApi = { state, dispatch, summaries, now, closeTab, startRename, receiveSummary, overlayOpen: !!switcher || overviewOpen || !!rename };
  const actionsFor = (pane: Pane): PaneActions => ({
    onDraft: draft => dispatch({ type: 'draft', id: pane.id, draft }),
    onView: change => dispatch({ type: 'view', id: pane.id, change }),
    onSummary: summary => receiveSummary(pane.id, summary),
    onOpenTaskTab: (taskId, title) => openTaskTab(pane, taskId, title),
  });
  return <section className="tab-workspace" aria-label="Conversation workspace">
    <TabStrip api={api} leading={leading} overviewTrigger={overviewTrigger} onOverview={() => setOverviewOpen(true)}/>
    <PaneGrid tab={active} dispatch={dispatch} actionsFor={actionsFor}/>
    {switcher && <div className="workspace-switcher"><div ref={switcherFocus} className="workspace-switcher-list" role="listbox" tabIndex={0} aria-label="Switch tabs" aria-activedescendant={`switcher-${switcher.ids[switcher.index]}`}>
      {switcher.ids.map((id, index) => { const tab = state.tabs.find(t => t.id === id); return tab ? <Button key={id} id={`switcher-${id}`} className="workspace-switcher-item" role="option" aria-selected={index === switcher.index} tabIndex={-1} onClick={() => { dispatch({ type: 'select', id }); switcherRef.current = null; setSwitcher(null); }}><Icon name={tab.pinned ? 'pin' : kindDef(focusedPane(tab).kind).icon} size="sm"/><span>{tab.title}</span></Button> : null; })}
    </div><Text>Release Ctrl to switch · Escape to cancel</Text>
    </div>}
    <TabOverview summaries={summaries} returnFocus={overviewTrigger} open={overviewOpen} tabs={state.tabs} groups={state.groups} activeId={state.activeId} onClose={() => setOverviewOpen(false)} onSelect={id => dispatch({ type: 'select', id })} onNew={() => dispatch({ type: 'new' })} onPin={id => dispatch({ type: 'pin', id })} onMoveGroup={(id, groupId) => dispatch({ type: 'move-group', id, groupId })} onCreateGroup={id => dispatch({ type: 'group', id })} onCloseTab={id => dispatch({ type: 'close', id })}/>
    <dialog ref={renameDialog} className="workspace-rename" aria-label={rename?.group ? 'Rename group' : 'Rename tab'} onCancel={() => setRename(null)} onClose={() => setRename(null)}>
      <form onSubmit={event => { event.preventDefault(); if (rename) dispatch({ type: rename.group ? 'rename-group' : 'rename', id: rename.id, title: rename.value }); setRename(null); }}><TextInput ref={renameInput} aria-label="Name" value={rename?.value ?? ''} maxLength={80} onChange={event => setRename(current => current ? { ...current, value: event.target.value } : null)}/><div className="workspace-rename-actions"><Button onClick={() => setRename(null)}>Cancel</Button><Button type="submit" variant="quiet">Save</Button></div></form>
    </dialog>
  </section>;
}

// The group suggestion pill (Shell 2b): centred on the content card, one line on a wide frame.
// Group hands the suggestion to the reducer's own `group` action. Dismissal is this window's memory
// for the app session (suggest.ts); nothing here is written down.
import { useMemo, useReducer, useState } from 'react';
import { Button, Icon, IconButton } from '../../components/ui';
import { useWorld } from '../world/useWorld.ts';
import type { WorldClient } from '../world/worldClient.ts';
import type { TabsApi } from './context';
import { workspaceReducer, type WorkspaceState } from './model';
import { suggestGroup, suggestionSetKey, type GroupSuggestion } from './suggest.ts';
import type { Tab } from './types';
import '../../App.css';
import './suggest-pill.css';

/** World rows, stable until the feed replaces them. A new array every read would loop the store subscription. */
const selectWorldRows = (world: WorldClient) => world.rows();

/**
 * Dismissals for this app session. The pill is mounted inside the tab panel, which remounts when the
 * active tab changes, so the set cannot live in component state or the same suggestion would return.
 */
const sessionDismissed = new Set<string>();

type RowIndex = ReadonlyMap<string, { workspace?: string }>;

/**
 * Top-centre suggestion over the content card. Render inside the tab panel (`workspace-conversation` is
 * the positioning parent). With no suggestion it draws nothing.
 */
export function SuggestPill({ api, rows }: { api: Pick<TabsApi, 'state' | 'dispatch'>; rows?: RowIndex }) {
  const live = useWorld(selectWorldRows);
  const indexed = useMemo(() => rows ?? new Map(live.map(row => [row.chatId, { workspace: row.workspace }])), [rows, live]);
  const [version, setVersion] = useState(0);
  const suggestion = useMemo(
    () => suggestGroup(api.state.tabs, api.state.groups, indexed, sessionDismissed),
    [api.state.tabs, api.state.groups, indexed, version],
  );
  if (!suggestion) return null;
  const count = suggestion.ids.length;
  const hide = (offer: GroupSuggestion) => {
    sessionDismissed.add(suggestionSetKey(offer.ids));
    setVersion(value => value + 1);
  };
  return <div className="suggest-pill" role="status">
    <span className="suggest-pill-mark"><Icon name="layers" size="xs"/></span>
    <span className="suggest-pill-text">Group the {count} {suggestion.title} tabs as <b>{suggestion.title}</b>?</span>
    <Button variant="primary" className="suggest-pill-group" onClick={() => { api.dispatch({ type: 'group', id: suggestion.ids[0], ids: suggestion.ids.slice(1), title: suggestion.title }); hide(suggestion); }}>Group</Button>
    <IconButton className="suggest-pill-dismiss" size="row" icon="close" iconSize="micro" label="Dismiss" onClick={() => hide(suggestion)}/>
  </div>;
}

function specimenTabs(count: number): WorkspaceState {
  const tabs: Tab[] = Array.from({ length: count }, (_, index) => ({
    id: String(index), kind: 'conversation', title: `Tab ${index}`, draft: '', pinned: false,
    sessionFile: `/chats/${index}/transcript.jsonl`,
  }));
  return { tabs, groups: [], activeId: tabs[0]?.id ?? '', closed: [], nextNumber: count + 1, recentIds: tabs.map(tab => tab.id) };
}

/** The measurement page (`?specimen=suggest-pill`). `count` and `folder` build the suggestion; the product never opens this. */
export function SuggestPillSpecimen() {
  const params = new URLSearchParams(window.location.search);
  const folder = params.get('folder') || 'bench';
  const count = Number(params.get('count') ?? '3');
  const size = Number.isInteger(count) && count > 0 ? count : 0;
  const [state, dispatch] = useReducer(workspaceReducer, size, specimenTabs);
  const rows = useMemo(() => {
    const index: RowIndex = new Map(Array.from({ length: size }, (_, index) => [String(index), { workspace: `/work/${folder}` }] as const));
    return index;
  }, [size, folder]);
  const grouped = state.tabs.filter(tab => tab.groupId).length;
  return <div className="suggest-pill-card" data-group-title={state.groups[0]?.title ?? ''} data-grouped-count={grouped}>
    <SuggestPill api={{ state, dispatch }} rows={rows}/>
  </div>;
}

// This fixture is mounted only by the browser test entry, never by the desktop app.
import { useState } from 'react';
import { Button } from '../../../components/ui';
import '../home.css';
import { LiveRows, type LiveItem } from './LiveRows';
import { buildHomeActions } from '../shell/homeActions';
import type { PlacesShell } from '../shell/PlacesShell';
import type { HomeDigest } from '../client';
import { freshWorkspace, workspaceReducer } from '../../tabs/model';
import { newTab } from '../../tabs/helpers';

const items: LiveItem[] = [
  { id: 'council', title: 'Marketing with Software', detail: 'can the post promise trailing commas?', status: 'running', turns: { current: 2, total: 6 } },
  { id: 'publish', title: 'Publish the launch post?', detail: 'irreversible, always yours', status: 'waiting' },
  { id: 'closed', title: 'Closed work', status: 'running' },
];
function initial() {
  let state = freshWorkspace({ id: 'launch', title: 'Launch' });
  for (const item of [items[0], items[2], items[1]]) state = workspaceReducer(state, { type: 'open', tab: newTab({ id: item.id, kind: 'conversation', title: item.title, sessionFile: `/fixture/${item.id}`, draft: 'kept draft' }), background: false });
  return workspaceReducer(state, { type: 'close', id: 'closed' });
}
export function LiveRowsHarness() {
  const [state, setState] = useState(initial);
  const [empty, setEmpty] = useState(false);
  const digest = { chats: items.map(item => ({ id: item.id, title: item.title, sessionFile: `/fixture/${item.id}` })) } as HomeDigest;
  const actions = buildHomeActions({ shell: { places: { status: 'ready' }, native: { desktop: false } } as PlacesShell, homeId: 'launch', digests: [digest], strip: { state, dispatch: action => setState(before => workspaceReducer(before, action)) }, quickLook: () => {}, retry: () => {} });
  return <div className="home-column">
    <LiveRows items={empty ? [] : items} readOnly={new URLSearchParams(location.search).has('readonly')} onOpen={actions.openChat} onOpenInNewTab={actions.openChatInNewTab}/>
    <output hidden aria-label="Workspace state">{JSON.stringify({ ids: state.tabs.map(tab => tab.id), active: state.activeId, closed: state.closed.length, draft: state.tabs.find(tab => tab.id === 'closed')?.draft })}</output>
    <Button onClick={() => setEmpty(true)}>Clear fixture</Button>
  </div>;
}

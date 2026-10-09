import { useContext, useSyncExternalStore } from 'react';
import { Button, SectionLabel } from '../../components/ui';
import { worldStore } from '../chat/world-store';
import type { WorldRow } from '../chat/world-client';
import { AttentionList, AttentionRow } from '../places/components/AttentionRow';
import { HomeTitle } from '../../components/ui';
import { newTab, panesOf } from '../tabs/helpers';
import { NewTabHostContext } from '../tabs/kinds/newtab/api';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import './inbox.css';

/**
 * The Inbox (Places 6d "Something needs you in another place", Shell 3j "Inbox, background work"): every question
 * the engine is waiting on, then the conversations running with no tab open on them, read from the engine-wide world
 * stream. Clicking a row opens (or focuses) that conversation in this strip. Nothing here is a sample and nothing is
 * answered from here yet: answering waits in the conversation's own tray, so the row says "needs you" and opens it.
 * A row's source folders are a conversation's filesystem folders, not places, so no place name is claimed for it.
 */
export function InboxPane(_props: PaneRenderProps) {
  const world = useSyncExternalStore(worldStore.subscribe, worldStore.getState);
  const strip = useContext(NewTabHostContext);
  const rowOf = new Map(world.rows.map(row => [row.session, row] as const));
  const openIn = new Set(strip ? strip.state.tabs.flatMap(tab => panesOf(tab).map(pane => pane.sessionFile)).filter(Boolean) : []);
  // Every attention item is a question a conversation is stopped on; its kind is the question's own (consent, choice…).
  const waiting = world.items;
  const background = world.rows.filter(row => row.running && !row.archived && !(row.sessionFile && openIn.has(row.sessionFile)));
  const open = (row: WorldRow | undefined, background = false) => {
    if (!strip || !row?.sessionFile) return;
    const existing = strip.state.tabs.flatMap(tab => panesOf(tab)).find(pane => pane.sessionFile === row.sessionFile);
    if (existing) { if (!background) strip.dispatch({ type: 'select', id: existing.id }); return; }
    strip.dispatch({ type: 'open', tab: newTab({ kind: 'conversation', title: row.title || 'Untitled chat', titleSource: 'engine', sessionFile: row.sessionFile }), background });
  };
  const nothing = !waiting.length && !background.length;
  return <div className="inbox-pane"><div className="inbox-column">
    <HomeTitle>Inbox</HomeTitle>
    {world.status === 'unavailable' && <p className="inbox-quiet" role="status">Can’t reach the engine. {world.error ?? ''} <Button variant="ghost" onClick={() => worldStore.retryNow()}>Retry</Button></p>}
    {world.status !== 'unavailable' && nothing && <p className="inbox-quiet">Nothing needs you, and nothing is running in the background.</p>}
    {waiting.length > 0 && <section className="inbox-section" aria-label="Needs you">
      <SectionLabel>Needs you</SectionLabel>
      <AttentionList label="Needs you">
        {waiting.map(item => {
          const row = rowOf.get(item.session);
          return <AttentionRow key={item.key} id={item.key} title={item.title || row?.title || 'Untitled chat'} status="waiting" statusText={item.text || undefined}
            disabled={!row?.sessionFile || !strip} onOpen={() => open(row)} onOpenInNewTab={() => open(row, true)}/>;
        })}
      </AttentionList>
    </section>}
    {background.length > 0 && <section className="inbox-section" aria-label="Running in the background">
      <SectionLabel>Running in the background</SectionLabel>
      <AttentionList label="Running in the background">
        {background.map(row => <AttentionRow key={row.session} id={row.session} title={row.title || 'Untitled chat'} status="running" statusText={row.state}
          disabled={!row.sessionFile || !strip} onOpen={() => open(row)} onOpenInNewTab={() => open(row, true)}/>)}
      </AttentionList>
    </section>}
  </div></div>;
}

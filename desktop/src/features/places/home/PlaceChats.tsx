import type { KeyboardEvent } from 'react';
import { SectionLabel } from '../../../components/ui';
import { ChatList, ChatRow } from '../components/ChatRow';
import { shortTime, type HomeChat } from '../home-model';
import { chatMenu, writeDrag, type DropPayload, type PlaceActions } from '../place-actions';
import { useRunner, type DragState } from '../HomeSections';

function moveFocus(event: KeyboardEvent<HTMLButtonElement>) {
  const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0;
  if (!step) return;
  const rows = [...event.currentTarget.closest('ul')!.querySelectorAll<HTMLButtonElement>('.places-row-main:not(:disabled)')];
  const next = rows[rows.indexOf(event.currentTarget) + step];
  if (next) { event.preventDefault(); next.focus(); }
}

/** The shared row draws engine evidence, while the section keeps opening failures visible beside the list. */
export function PlaceChats({ label, chats, truncated, actions, readOnly, inPlaceId, drag, now }: {
  label: string; chats: readonly HomeChat[]; truncated?: boolean; actions: PlaceActions; readOnly?: boolean; inPlaceId?: string; drag: DragState; now: Date;
}) {
  const runner = useRunner();
  if (!chats.length) return null;
  const draggable = !readOnly && !!actions.file;
  return <section className="home-section home-chats" aria-label={label}>
    <SectionLabel>{label}</SectionLabel>
    <ChatList label={label}>
      {chats.map(chat => <ChatRow key={chat.id} id={chat.id} title={chat.title || 'Untitled chat'} excerpt={chat.excerpt} status={chat.status} model={chat.model}
        timeLabel={shortTime(chat.at, now)} timeIso={chat.at} disabled={!actions.openChat} onKeyDown={moveFocus}
        onOpen={() => void runner.run(() => actions.openChat?.(chat.id))} onOpenInNewTab={actions.openChatInNewTab && (() => void runner.run(() => actions.openChatInNewTab?.(chat.id)))}
        menu={chatMenu(chat.id, actions, { readOnly, inPlaceId })} draggable={draggable} dragging={drag.payload?.kind === 'chat' && drag.payload.ids.includes(chat.id)}
        onDragStart={event => { const payload: DropPayload = { kind: 'chat', ids: [chat.id] }; writeDrag(event, payload); drag.set(payload); }} onDragEnd={() => drag.set(undefined)}/>)}
    </ChatList>
    {runner.error && <p className="home-quiet" role="alert">{runner.error}</p>}
    {truncated && <p className="home-quiet">Showing the most recent chats.</p>}
  </section>;
}

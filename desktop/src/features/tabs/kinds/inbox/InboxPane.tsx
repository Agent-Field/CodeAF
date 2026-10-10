import { useId } from 'react';
import { Button, Text } from '../../../../components/ui';
import { compactAge, type BackgroundWork } from '../../closing/background';
import { useTabsApi } from '../../context';
import './inbox.css';

type RowItem = BackgroundWork['running'][number] | BackgroundWork['needsYou'][number];
type ListProps = BackgroundWork & {
  now: number;
  /** Where a row goes when clicked, or undefined when it can go nowhere: such a row is plain text, never a dead button. */
  opener?: (item: RowItem, section: 'running' | 'needsYou') => (() => void) | undefined;
};

type RowProps = { state: 'running' | 'waiting'; title: string; meta?: string; stale?: boolean; onClick?: () => void };
function InboxRow({ state, title, meta, stale, onClick }: RowProps) {
  const body = <><span className="inbox-dot" aria-hidden="true"/><span className="inbox-title">{title}</span>{meta && <span className="inbox-meta">{meta}</span>}</>;
  return onClick
    ? <Button className="inbox-row" data-state={state} data-stale={stale || undefined} onClick={onClick}>{body}</Button>
    : <div className="inbox-row inbox-row-static" data-state={state} data-stale={stale || undefined}>{body}</div>;
}

/**
 * The Inbox card (design 3l "Inbox, background work"). "Running in the background" lists conversations whose work goes on
 * with no tab open for them; a click reopens the tab where it was. "Needs you" lists conversations waiting on a question.
 * With none of them, one muted line says what arrives here.
 * Presentational: the pane and the Design system specimen both draw it.
 */
export function InboxList({ running, needsYou, notice, now, opener }: ListProps) {
  const empty = !running.length && !needsYou.length;
  const uid = useId();
  return (
    <div className="inbox-card" role="region" aria-label="Inbox">
      {running.length > 0 && (
        <section className="inbox-section" aria-labelledby={`${uid}-running`}>
          <h3 className="inbox-head" id={`${uid}-running`}>Running in the background</h3>
          <ul className="inbox-list">
            {running.map(item => (
              <li key={item.id}>
                <InboxRow state={item.state} title={item.title} stale={item.stale} meta={item.state === 'waiting' ? 'needs you' : item.since ? compactAge(now - item.since) : ''} onClick={opener?.(item, 'running')}/>
              </li>
            ))}
          </ul>
        </section>
      )}
      {needsYou.length > 0 && (
        <section className="inbox-section" data-section="needsYou" aria-labelledby={`${uid}-needs`}>
          <h3 className="inbox-head" id={`${uid}-needs`}>Needs you</h3>
          <ul className="inbox-list">
            {needsYou.map(item => (
              <li key={item.id}>
                <InboxRow state="waiting" title={item.title} stale={item.stale} meta="needs you" onClick={opener?.(item, 'needsYou')}/>
              </li>
            ))}
          </ul>
        </section>
      )}
      {notice && <Text className="inbox-notice" role="status">{notice}</Text>}
      {empty && !notice && <Text className="inbox-empty">Work that needs you, or keeps running after you close its tab, lands here.</Text>}
    </div>
  );
}

/** The Inbox tab's body: the card, fed by the workspace's real session reads and the engine's world feed. */
export function InboxPane() {
  const api = useTabsApi();
  const { background, openChat } = api;
  /** Goes to the conversation: its tab if this window has one (reopened if closed), else the shell's opener, else nowhere. */
  const opener = (item: { tabId?: string; chatId?: string }) => {
    const { tabId, chatId } = item;
    return tabId ? () => (api.state.tabs.some(t => t.id === tabId) ? api.dispatch({ type: 'select', id: tabId }) : api.reopenClosed(tabId))
      : chatId && openChat && (api.canOpenChat?.(chatId) ?? true) ? () => openChat(chatId) : undefined;
  };
  return (
    <div className="inbox">
      <InboxList {...background} now={api.now} opener={opener}/>
    </div>
  );
}

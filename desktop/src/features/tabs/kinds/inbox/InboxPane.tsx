import { useId } from 'react';
import { Button, Text } from '../../../../components/ui';
import { compactAge, type BackgroundWork } from '../../closing/background';
import { useTabsApi } from '../../context';
import './inbox.css';

type ListProps = BackgroundWork & {
  now: number;
  onReopen?: (id: string) => void;
  onSelect?: (id: string) => void;
};

/**
 * The Inbox card (design 3l "Inbox, background work"). "Running in the background" lists closed tabs whose work
 * goes on; a click reopens the tab where it was. "Needs you" lists open tabs waiting on a question. With neither,
 * one muted line says what arrives here. Presentational: the pane and the Design system specimen both draw it.
 */
export function InboxList({ running, needsYou, now, onReopen, onSelect }: ListProps) {
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
                <Button className="inbox-row" data-state={item.state} onClick={() => onReopen?.(item.id)}>
                  <span className="inbox-dot" aria-hidden="true"/>
                  <span className="inbox-title">{item.title}</span>
                  <span className="inbox-meta">{item.state === 'waiting' ? 'needs you' : item.since ? compactAge(now - item.since) : ''}</span>
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {needsYou.length > 0 && (
        <section className="inbox-section" aria-labelledby={`${uid}-needs`}>
          <h3 className="inbox-head" id={`${uid}-needs`}>Needs you</h3>
          <ul className="inbox-list">
            {needsYou.map(item => (
              <li key={item.id}>
                <Button className="inbox-row" data-state="waiting" onClick={() => onSelect?.(item.id)}>
                  <span className="inbox-dot" aria-hidden="true"/>
                  <span className="inbox-title">{item.title}</span>
                  <span className="inbox-meta">needs you</span>
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {empty && <Text className="inbox-empty">Work that needs you, or keeps running after you close its tab, lands here.</Text>}
    </div>
  );
}

/** The Inbox tab's body: the card, fed by the workspace's real session reads. */
export function InboxPane() {
  const api = useTabsApi();
  return <div className="inbox"><InboxList {...api.background} now={api.now} onReopen={api.reopenClosed} onSelect={id => api.dispatch({ type: 'select', id })}/></div>;
}

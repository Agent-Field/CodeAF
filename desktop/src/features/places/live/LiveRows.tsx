import { Button, SectionLabel, StatusMark } from '../../../components/ui';
import type { HomeAttention } from '../home-model';
import './live.css';

export type LiveItem = HomeAttention & {
  /** The engine's current question or discussion topic, independent of its title. */
  detail?: string;
  /** Counts are supplied together so an unknown council limit is never guessed. */
  turns?: { current: number; total: number };
};

type Props = {
  items: readonly LiveItem[];
  onOpen?: (id: string) => void;
  onOpenInNewTab?: (id: string) => void;
  readOnly?: boolean;
};

/** Live work belongs to the place, including work whose view has been closed. The feed's order is retained. */
export function LiveRows({ items, onOpen, onOpenInNewTab, readOnly }: Props) {
  if (!items.length) return null;
  return <section className="home-section home-live" aria-label="Live">
    <SectionLabel>Live</SectionLabel>
    <ul className="home-live-list" aria-label="Live work">
      {items.map(item => {
        const running = item.status === 'running';
        const turns = item.turns;
        const progress = turns && Number.isInteger(turns.current) && Number.isInteger(turns.total)
          && turns.current >= 0 && turns.total > 0 && turns.current <= turns.total
          ? `${turns.current} of ${turns.total} turns` : undefined;
        const aside = progress ?? item.statusText ?? (running ? 'running' : item.status === 'waiting' ? 'needs you' : 'failed');
        return <li key={`${item.status}:${item.id}`} data-attention-id={item.id} data-status={item.status}>
          <Button variant="ghost" className="home-live-row" disabled={readOnly || !onOpen}
            onClick={event => { if ((event.metaKey || event.ctrlKey) && onOpenInNewTab) onOpenInNewTab(item.id); else onOpen?.(item.id); }}
            onAuxClick={event => { if (event.button === 1 && onOpenInNewTab) { event.preventDefault(); onOpenInNewTab(item.id); } }}>
            <StatusMark dense status={item.status} label={running ? 'Running' : item.status === 'waiting' ? 'Needs you' : 'Failed'}/>
            <span className="home-live-text"><span className={running ? 'home-live-title ui-live-shimmer' : 'home-live-title'}>{item.title}</span>
              {item.detail && <span className="home-live-detail"> · {item.detail}</span>}
              {item.placeName && <span className="home-live-detail"> · in {item.placeName}</span>}
            </span>
            {aside && <span className="home-live-aside">{aside}</span>}
          </Button>
        </li>;
      })}
    </ul>
  </section>;
}

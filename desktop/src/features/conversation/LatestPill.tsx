import { Button, Icon } from '../../components/ui';
import type { ConversationModel } from './types';
import { elapsed } from './work/format';
import { useNow } from './work/useNow';
import './latest-pill.css';

/** When the running turn's live work began; unknown when the engine has not said. */
export function liveSince(model: ConversationModel): number | undefined {
  const blocks = model.turns[model.turns.length - 1]?.blocks ?? [];
  const live = blocks.find((block) => block.kind === 'work' && block.live);
  return live?.kind === 'work' ? live.startedAt : undefined;
}

type Props = { working: boolean; since?: number; now?: number; onJump: () => void };

/**
 * "Latest · Working 1m 13s": offered only while the reader is away from the end
 * and something is live or new below. The shimmer marks the live part; with
 * nothing live the pill is just "Latest" (design 1f, Scroll behaviour).
 */
export function LatestPill({ working, since, now: given, onJump }: Props) {
  const now = useNow(working, given);
  const time = working ? elapsed(since, now) : undefined;
  return (
    <Button className="latest-pill" onClick={onJump}>
      <Icon name="arrowDown" size="xs" />
      <span className="latest-pill-label">Latest</span>
      {working && (
        <>
          <span className="latest-pill-dot" aria-hidden="true">·</span>
          <span className="latest-pill-shimmer">Working</span>
          {time && <span className="latest-pill-time">{time}</span>}
        </>
      )}
    </Button>
  );
}

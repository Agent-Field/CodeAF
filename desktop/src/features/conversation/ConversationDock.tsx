// Everything docked under the conversation: the decision tray, queued
// messages and the composer, in that order from the top.

import { useLayoutEffect, useRef, useState, type ComponentProps } from 'react';
import type { EngineAnswer, EngineQuestion } from '../chat/engine-client';
import { ImageFigure } from './assets';
import { QueuedRows, type QueuedItem } from './blocks/QueuedRows';
import { Composer } from './Composer';
import { CompactComposer } from './composer/CompactComposer';
import { EmptyStart } from './EmptyStart';
import { deadlineAt } from './tray/clock';
import { DecisionTray } from './tray/DecisionTray';
import { blocksComposer } from './tray/layout';
import { PaneMiniTray } from './tray/PaneMiniTray';
import { useNow } from './tasks/useNow';

type TrayProps = {
  questions: EngineQuestion[];
  busyKey: string | null;
  onAnswer: (answer: EngineAnswer) => Promise<boolean>;
  onHold: (question: { kind: string; id: number; ref?: string }) => void;
  focusKey?: string;
  compact?: boolean;
  onReview?: () => void;
};

type QueueProps = { items: QueuedItem[]; onRemove: (id: string) => void; onEdit: (id: string, text: string) => void; onMove: (id: string, to: number) => void; onSendNow: (id: string) => void };

/** In an unfocused pane of a split the dock shows the 36px compact field (labelled with the conversation) in place of the tray and composer. */
type Props = { tray: TrayProps; queue: QueueProps; composer: ComponentProps<typeof Composer>; compact?: { label: string } };

const BLOCKED = 'The reply is waiting on your answer above';

/** The tray's clock ticks once a second, and only while some question has a deadline. */
function Tray({ tray }: { tray: TrayProps }) {
  const timed = tray.questions.some((question) => deadlineAt(question) !== null);
  const now = useNow(timed);
  return (
    <DecisionTray
      {...tray}
      now={now}
      renderImage={(block) => (block.path ? <ImageFigure path={block.path} caption="" meta="" /> : null)}
    />
  );
}

function Queue({ queue }: { queue: QueueProps }) {
  return <QueuedRows items={queue.items} onRemove={queue.onRemove} onEdit={queue.onEdit} onMove={queue.onMove} onSendNow={queue.onSendNow} />;
}

/** Keeps the full composer mounted while compact, and hands it the keyboard (and a 200ms expand) when the pane takes focus. */
function useExpandOnFocus(compact: boolean) {
  const slot = useRef<HTMLDivElement>(null);
  const was = useRef(compact);
  const [expanding, setExpanding] = useState(false);
  const take = () => {
    const field = slot.current?.querySelector('textarea');
    field?.focus({ preventScroll: true });
    field?.setSelectionRange(field.value.length, field.value.length);
  };
  useLayoutEffect(() => {
    const expanded = was.current && !compact;
    was.current = compact;
    if (!expanded) return;
    setExpanding(true);
    // Keys typed into the compact field must land in the full one without a gap, so take the keyboard now; and again after the
    // click that focused the pane (the compact field it landed on is gone), which would otherwise leave focus on the body.
    take();
    const frame = requestAnimationFrame(take);
    return () => cancelAnimationFrame(frame);
  }, [compact]);
  return { slot, expanding, onEnd: () => setExpanding(false) };
}

export function ConversationDock({ tray, queue, composer, compact }: Props) {
  const blocked = blocksComposer(tray.questions);
  const expand = useExpandOnFocus(Boolean(compact));
  // Conversation 1b folds the queued rows into the toolbar chip once the reader
  // is more than a viewport from the end — the same distance the question tray
  // uses for its compact bar. Clicking the chip re-anchors, and that is what
  // brings the rows back. An unfocused split pane keeps the rows: its compact
  // field has no model-chip toolbar to put the chip in. An empty queue draws neither.
  const queueFolded = Boolean(tray.compact) && !compact && queue.items.length > 0 && tray.onReview !== undefined;
  // The rows come back in the same turn and shorten the reading pane, so one
  // jump lands short of the end. The second runs after that layout.
  const openQueue = () => {
    const review = tray.onReview;
    if (!review) return;
    review();
    requestAnimationFrame(review);
  };
  return (
    <>
      {!compact && <Tray tray={tray} />}
      {!queueFolded && <Queue queue={queue} />}
      {!composer.docked && <EmptyStart />}
      {compact && <CompactComposer label={compact.label} draft={composer.draft} onDraft={composer.onDraft}><PaneMiniTray questions={tray.questions} onReview={tray.onReview} /></CompactComposer>}
      <div ref={expand.slot} className="composer-slot" data-compact={compact ? '' : undefined} data-expanding={expand.expanding ? '' : undefined} onAnimationEnd={expand.onEnd}>
        <Composer
          {...composer}
          disabledReason={blocked ? BLOCKED : composer.disabledReason}
          queueChip={queueFolded && tray.onReview ? { count: queue.items.length, onOpen: openQueue } : undefined}
        />
      </div>
    </>
  );
}

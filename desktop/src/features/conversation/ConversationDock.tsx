// Everything docked under the conversation: the decision tray, queued
// messages and the composer, in that order from the top.

import type { ComponentProps } from 'react';
import { Text } from '../../components/ui';
import type { EngineAnswer, EngineQuestion } from '../chat/engine-client';
import { ImageFigure } from './assets';
import { QueuedRows, type QueuedItem } from './blocks/QueuedRows';
import { Composer } from './Composer';
import { deadlineAt } from './tray/clock';
import { DecisionTray } from './tray/DecisionTray';
import { blocksComposer } from './tray/layout';
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

type QueueProps = { items: QueuedItem[]; onRemove: (id: string) => void; removedHere: boolean };

type Props = { tray: TrayProps; queue: QueueProps; composer: ComponentProps<typeof Composer> };

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
  return (
    <>
      <QueuedRows items={queue.items} onRemove={queue.onRemove} />
      {queue.removedHere && (
        <Text className="queued-note" role="status">
          Removed here only. codeaf still sends a queued message after this turn.
        </Text>
      )}
    </>
  );
}

export function ConversationDock({ tray, queue, composer }: Props) {
  const blocked = blocksComposer(tray.questions);
  return (
    <>
      <Tray tray={tray} />
      <Queue queue={queue} />
      <Composer {...composer} disabledReason={blocked ? BLOCKED : composer.disabledReason} />
    </>
  );
}

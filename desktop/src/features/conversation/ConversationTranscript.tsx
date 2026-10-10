import type { ReactNode } from 'react';
import { Markdown } from '../../components/ui';
import { useAssetMarkdownHooks } from './assets';
import { AsideRow } from './AsideRow';
import { renderAttachment } from './blocks/AttachmentView';
import { TurnViewV2 } from './blocks/TurnViewV2';
import { workDisclosureKey, blockRenderer, type BlockContext } from './blockRenderer';
import { EarlierRow, SummaryDivider } from './EarlierTurns';
import { ErrorItem } from './ErrorItem';
import { EARLIER_KEY, hasCompaction, isFolded, splitEarlier } from './folding';
import { NoteItem } from './NoteItem';
import { TaskNotice } from './TaskNotice';
import { UserMessage } from './UserMessage';
import type { ConversationModel, TurnItem, TurnV2 } from './types';
import type { FailedSend } from './useConversation';

type Props = BlockContext & {
  model: ConversationModel;
  firstReplyAside?: ReactNode;
  folded: Record<string, boolean>;
  onToggleFold: (turnId: string, folded: boolean) => void;
  failed?: FailedSend;
  /** Words sent but not yet recorded. */
  sending?: string;
  onRetry: () => void;
  onOpenConversationTab?: (anchor: string, background: boolean) => void;
};

type PrefaceProps = Pick<BlockContext, 'open' | 'onToggle' | 'onOpenTask' | 'tasks' | 'onPause' | 'onResume' | 'onStop'> & { item: TurnItem };

function PrefaceItem({ item, open, onToggle, onOpenTask, tasks, onPause, onResume, onStop }: PrefaceProps) {
  const hooks = useAssetMarkdownHooks();
  const expanded = Boolean(open[item.id]);
  switch (item.kind) {
    case 'note':
      return <NoteItem text={item.text} long={item.long} tone={item.tone} undoReceipts={item.undoReceipts} />;
    case 'text':
      return <Markdown {...hooks}>{item.text}</Markdown>;
    case 'aside':
      return <AsideRow item={item} open={expanded} onToggle={() => onToggle(item.id)} />;
    case 'task':
      return (
        <TaskNotice
          item={item}
          open={expanded}
          onToggle={() => onToggle(item.id)}
          onOpenTask={onOpenTask}
          tasks={tasks}
          onPause={onPause}
          onResume={onResume}
          onStop={onStop}
        />
      );
    default:
      return null;
  }
}

/** The conversation itself: notes recorded before the first message, then the turns. */
export function ConversationTranscript(props: Props) {
  const { model, folded, onToggleFold, failed, sending, onRetry } = props;
  const lastId = model.turns[model.turns.length - 1]?.id;
  const renderFor = blockRenderer(props);
  const { earlier, recent } = splitEarlier(model.turns);
  const earlierOpen = Boolean(props.open[EARLIER_KEY]);
  const renderTurn = (turn: TurnV2) => {
    const isFold = isFolded(turn, model.turns.indexOf(turn), model.turns.length, folded);
    return (
      <TurnViewV2
        key={turn.id}
        turn={turn}
        onOpenConversationTab={props.onOpenConversationTab}
        afterReply={turn === model.turns[0] ? props.firstReplyAside : undefined}
        folded={isFold}
        onToggleFold={() => onToggleFold(turn.id, !isFold)}
        renderBlock={renderFor(turn)}
        onOpenWork={(ids) => turn.blocks.filter(block => block.kind === 'work' && ids.includes(block.id)).forEach(block => {
          if (block.kind !== 'work') return;
          const key = workDisclosureKey(turn, block);
          if (!props.open[key]) props.onToggle(key, false);
        })}
        renderAttachment={renderAttachment}
        onRetry={turn.id === lastId ? onRetry : undefined}
      />
    );
  };
  return (
    <>
      {model.preface.length > 0 && (
        <div className="conversation-preface">
          {model.preface.map((item) => (
            <PrefaceItem key={item.id} item={item} open={props.open} onToggle={props.onToggle} onOpenTask={props.onOpenTask} tasks={props.tasks} onPause={props.onPause} onResume={props.onResume} onStop={props.onStop} />
          ))}
        </div>
      )}
      {earlier.length > 0 && (
        <>
          <EarlierRow count={earlier.length} open={earlierOpen} onToggle={props.onToggle} />
          {earlierOpen && earlier.map(renderTurn)}
          {hasCompaction(model) && <SummaryDivider />}
        </>
      )}
      {recent.map(renderTurn)}
      {sending && (
        <section className="turn-v2" data-pending="true">
          <UserMessage text={sending} sending />
        </section>
      )}
      {failed && (
        <div className="conversation-failure">
          <ErrorItem text={failed.message} onRetry={failed.text || failed.files?.length ? onRetry : undefined} />
        </div>
      )}
    </>
  );
}

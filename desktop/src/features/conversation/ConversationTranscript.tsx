import { Markdown } from '../../components/ui';
import { useAssetMarkdownHooks } from './assets';
import { AsideRow } from './AsideRow';
import { renderAttachment } from './blocks/AttachmentView';
import { TurnViewV2 } from './blocks/TurnViewV2';
import { blockRenderer, type BlockContext } from './blockRenderer';
import { EarlierRow, SummaryDivider } from './EarlierTurns';
import { ErrorItem } from './ErrorItem';
import { EARLIER_KEY, isFolded, splitEarlier } from './folding';
import { NoteItem } from './NoteItem';
import { TaskNotice } from './TaskNotice';
import type { ConversationModel, TurnItem, TurnV2 } from './types';
import type { FailedSend } from './useConversation';

type Props = BlockContext & {
  model: ConversationModel;
  folded: Record<string, boolean>;
  onToggleFold: (turnId: string, folded: boolean) => void;
  failed?: FailedSend;
  onRetry: () => void;
};

type PrefaceProps = Pick<BlockContext, 'open' | 'onToggle' | 'onOpenTask'> & { item: TurnItem };

function PrefaceItem({ item, open, onToggle, onOpenTask }: PrefaceProps) {
  const hooks = useAssetMarkdownHooks();
  const expanded = Boolean(open[item.id]);
  switch (item.kind) {
    case 'note':
      return <NoteItem text={item.text} long={item.long} />;
    case 'text':
      return <Markdown {...hooks}>{item.text}</Markdown>;
    case 'aside':
      return <AsideRow item={item} open={expanded} onToggle={() => onToggle(item.id)} />;
    case 'task':
      return <TaskNotice item={item} open={expanded} onToggle={() => onToggle(item.id)} onOpenTask={onOpenTask} />;
    default:
      return null;
  }
}

/** The conversation itself: notes recorded before the first message, then the turns. */
export function ConversationTranscript(props: Props) {
  const { model, folded, onToggleFold, failed, onRetry } = props;
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
        folded={isFold}
        onToggleFold={() => onToggleFold(turn.id, !isFold)}
        renderBlock={renderFor(turn)}
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
            <PrefaceItem key={item.id} item={item} open={props.open} onToggle={props.onToggle} onOpenTask={props.onOpenTask} />
          ))}
        </div>
      )}
      {earlier.length > 0 && (
        <>
          <EarlierRow count={earlier.length} open={earlierOpen} onToggle={props.onToggle} />
          {earlierOpen && earlier.map(renderTurn)}
          <SummaryDivider />
        </>
      )}
      {recent.map(renderTurn)}
      {failed && (
        <div className="conversation-failure">
          <ErrorItem text={failed.message} onRetry={failed.text || failed.files?.length ? onRetry : undefined} />
        </div>
      )}
    </>
  );
}

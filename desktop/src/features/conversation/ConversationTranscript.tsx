import { Markdown } from '../../components/ui';
import { useAssetMarkdownHooks } from './assets';
import { AsideRow } from './AsideRow';
import { renderAttachment } from './blocks/AttachmentView';
import { TurnViewV2 } from './blocks/TurnViewV2';
import { blockRenderer, type BlockContext } from './blockRenderer';
import { ErrorItem } from './ErrorItem';
import { NoteItem } from './NoteItem';
import { TaskNotice } from './TaskNotice';
import type { ConversationModel, TurnItem } from './types';
import type { FailedSend } from './useConversation';

type Props = BlockContext & {
  model: ConversationModel;
  folded: Record<string, boolean>;
  onToggleFold: (turnId: string) => void;
  failed?: FailedSend;
  onRetry: () => void;
};

type PrefaceProps = Pick<BlockContext, 'open' | 'onToggle' | 'onOpenTask'> & { item: TurnItem };

function PrefaceItem({ item, open, onToggle, onOpenTask }: PrefaceProps) {
  const hooks = useAssetMarkdownHooks();
  const expanded = Boolean(open[item.id]);
  switch (item.kind) {
    case 'note':
      return <NoteItem text={item.text} long={item.long} tone={item.tone} />;
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
  return (
    <>
      {model.preface.length > 0 && (
        <div className="conversation-preface">
          {model.preface.map((item) => (
            <PrefaceItem key={item.id} item={item} open={props.open} onToggle={props.onToggle} onOpenTask={props.onOpenTask} />
          ))}
        </div>
      )}
      {model.turns.map((turn) => (
        <TurnViewV2
          key={turn.id}
          turn={turn}
          folded={Boolean(folded[turn.id])}
          onToggleFold={() => onToggleFold(turn.id)}
          renderBlock={renderFor(turn)}
          renderAttachment={renderAttachment}
          onRetry={turn.id === lastId ? onRetry : undefined}
        />
      ))}
      {failed && (
        <div className="conversation-failure">
          <ErrorItem text={failed.message} onRetry={failed.text || failed.files?.length ? onRetry : undefined} />
        </div>
      )}
    </>
  );
}

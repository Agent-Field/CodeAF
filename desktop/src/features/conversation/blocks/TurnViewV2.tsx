import type { ReactNode } from 'react';
import { Button, ContextMenu, Icon, IconButton } from '../../../components/ui';
import type { TurnBlock, TurnV2 } from '../types';
import { spoken } from '../work/format';
import { UserMessage } from '../UserMessage';
import { plainMessage } from '../composer/pastedText';
import { AnswerBlock, ErrorBlock, UpdateBlock, type WorkedLink } from './BlockViews';
import { Attachments, type RenderAttachment } from './attachments';
import { Steers } from './Steer';
import { TurnFooter } from './TurnFooter';
import { foldedTurnMenu } from '../menus/messageMenu';
import '../conversation.css';
import './blocks.css';

export type TurnViewV2Props = {
  turn: TurnV2;
  afterReply?: ReactNode;
  folded: boolean;
  onToggleFold: () => void;
  /** Draws the kinds this view does not own: work, task, deliverable, receipt. */
  renderBlock: (block: TurnBlock) => ReactNode;
  renderAttachment: RenderAttachment;
  onRetry?: () => void;
  retrying?: boolean;
  /** Opens the turn's work blocks; with it, the last answer carries a "Worked 42s" link. */
  onOpenWork?: (blockIds: string[]) => void;
  /** Opens the same conversation at this turn without replacing the reader's tab. */
  onOpenConversationTab?: (anchor: string, background: boolean) => void;
};

function FoldedLine({ turn, onToggleFold, onOpenConversationTab }: Pick<TurnViewV2Props, 'turn' | 'onToggleFold' | 'onOpenConversationTab'>) {
  return (
    <ContextMenu items={foldedTurnMenu(turn, onOpenConversationTab)} label="Folded turn actions">
      <Button className="turn-folded" aria-expanded={false} aria-label="Unfold" onClick={onToggleFold}>
        <span className="turn-folded-user">{plainMessage(turn.user)}</span>
        {turn.digest && <span className="turn-folded-digest">{turn.digest}</span>}
        <span className="turn-folded-chevron"><Icon name="chevron" size="xs" /></span>
      </Button>
    </ContextMenu>
  );
}

type BlockProps = Pick<TurnViewV2Props, 'renderBlock' | 'onRetry'> & { block: TurnBlock; worked?: WorkedLink };

function builtIn({ block, renderBlock, onRetry, worked }: BlockProps): ReactNode {
  if (block.kind === 'update') return <UpdateBlock block={block} />;
  if (block.kind === 'answer') return <AnswerBlock block={block} worked={worked} />;
  if (block.kind === 'error') return <ErrorBlock block={block} onRetry={onRetry} />;
  return renderBlock(block);
}

/** A block the renderer leaves out (an image drawn in its neighbour's grid) adds no gap. */
function Block(props: BlockProps) {
  const content = builtIn(props);
  if (content === null || content === undefined) return null;
  return (
    <div className="turn-block" data-kind={props.block.kind}>
      {content}
    </div>
  );
}

/** "Worked 42s" for the turn's work, opening every work block of the turn; absent without a measured length. */
function workedLink(turn: TurnV2, onOpenWork?: (blockIds: string[]) => void): WorkedLink | undefined {
  const blocks = turn.blocks.filter((block) => block.kind === 'work');
  const seconds = blocks.reduce((sum, block) => sum + (block.summary.seconds ?? 0), 0);
  if (!onOpenWork || seconds <= 0) return undefined;
  return { label: `Worked ${spoken(seconds)}`, onOpen: () => onOpenWork(blocks.map((block) => block.id)) };
}

/** The footer repeats Retry only when no error block already offers it. */
function footerState(turn: TurnV2): TurnV2['state'] {
  const hasError = turn.blocks.some((block) => block.kind === 'error');
  return turn.state === 'failed' && hasError ? 'done' : turn.state;
}

export function TurnViewV2(props: TurnViewV2Props) {
  const { turn, folded, onToggleFold, renderBlock, renderAttachment, onRetry, retrying, onOpenWork } = props;
  if (folded) {
    return (
      <section className="turn-v2" data-folded="true" data-turn={turn.id} data-anchor={turn.id}>
        <FoldedLine turn={turn} onToggleFold={onToggleFold} onOpenConversationTab={props.onOpenConversationTab} />
      </section>
    );
  }
  const lastAnswer = turn.blocks.filter((block) => block.kind === 'answer').pop();
  const worked = workedLink(turn, onOpenWork);
  return (
    <section className="turn-v2" data-turn={turn.id} data-anchor={turn.id}>
      <IconButton className="turn-fold" icon="chevron" iconSize="sm" label="Fold" aria-expanded={true} onClick={onToggleFold} />
      <UserMessage text={turn.user} attachments={<Attachments files={turn.attachments} render={renderAttachment} />} />
      <Steers steer={turn.steer} inWork={turn.blocks.flatMap((block) => (block.kind === 'work' ? block.notes : []))} />
      {turn.blocks.map((block) => (
        <Block key={block.id} block={block} renderBlock={renderBlock} onRetry={onRetry} worked={block === lastAnswer ? worked : undefined} />
      ))}
      <TurnFooter state={footerState(turn)} onRetry={onRetry} retrying={retrying} />
      {props.afterReply}
    </section>
  );
}

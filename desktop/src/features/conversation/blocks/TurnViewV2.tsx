import type { ReactNode } from 'react';
import { Button, IconButton } from '../../../components/ui';
import type { TurnBlock, TurnV2 } from '../types';
import { UserMessage } from '../UserMessage';
import { AnswerBlock, ErrorBlock, UpdateBlock } from './BlockViews';
import { Attachments, type RenderAttachment } from './attachments';
import { Steers } from './Steer';
import { TurnFooter } from './TurnFooter';
import '../conversation.css';
import './blocks.css';

export type TurnViewV2Props = {
  turn: TurnV2;
  folded: boolean;
  onToggleFold: () => void;
  /** Draws the kinds this view does not own: work, task, deliverable, receipt. */
  renderBlock: (block: TurnBlock) => ReactNode;
  renderAttachment: RenderAttachment;
  onRetry?: () => void;
  retrying?: boolean;
};

function FoldedLine({ turn, onToggleFold }: Pick<TurnViewV2Props, 'turn' | 'onToggleFold'>) {
  return (
    <Button className="turn-folded" aria-expanded={false} aria-label="Unfold" onClick={onToggleFold}>
      <span className="turn-folded-user">{turn.user}</span>
      {turn.digest && <span className="turn-folded-digest">{turn.digest}</span>}
    </Button>
  );
}

type BlockProps = Pick<TurnViewV2Props, 'renderBlock' | 'onRetry'> & { block: TurnBlock };

function builtIn({ block, renderBlock, onRetry }: BlockProps): ReactNode {
  if (block.kind === 'update') return <UpdateBlock block={block} />;
  if (block.kind === 'answer') return <AnswerBlock block={block} />;
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

/** The footer repeats Retry only when no error block already offers it. */
function footerState(turn: TurnV2): TurnV2['state'] {
  const hasError = turn.blocks.some((block) => block.kind === 'error');
  return turn.state === 'failed' && hasError ? 'done' : turn.state;
}

export function TurnViewV2(props: TurnViewV2Props) {
  const { turn, folded, onToggleFold, renderBlock, renderAttachment, onRetry, retrying } = props;
  if (folded) {
    return (
      <section className="turn-v2" data-folded="true" data-turn={turn.id} data-anchor={turn.id}>
        <FoldedLine turn={turn} onToggleFold={onToggleFold} />
      </section>
    );
  }
  return (
    <section className="turn-v2" data-turn={turn.id} data-anchor={turn.id}>
      <IconButton className="turn-fold" icon="chevron" iconSize="sm" label="Fold" aria-expanded={true} onClick={onToggleFold} />
      <UserMessage text={turn.user} attachments={<Attachments files={turn.attachments} render={renderAttachment} />} />
      <Steers steer={turn.steer} inWork={turn.blocks.flatMap((block) => (block.kind === 'work' ? block.notes : []))} />
      {turn.blocks.map((block) => (
        <Block key={block.id} block={block} renderBlock={renderBlock} onRetry={onRetry} />
      ))}
      <TurnFooter state={footerState(turn)} onRetry={onRetry} retrying={retrying} />
    </section>
  );
}

import { Button, CopyButton, Markdown, RowActions } from '../../../components/ui';
import { useAssetMarkdownHooks } from '../assets/markdownHooks';
import { ErrorItem } from '../ErrorItem';
import type { TurnBlock } from '../types';
import './turn-answer.css';

type Block<K extends TurnBlock['kind']> = Extract<TurnBlock, { kind: K }>;

/** An interim message from the AI to the person, marked as an aside to the work. */
export function UpdateBlock({ block }: { block: Block<'update'> }) {
  const hooks = useAssetMarkdownHooks();
  if (!block.text) return null;
  return (
    <div className="update-block">
      <span className="update-eyebrow">Update</span>
      <div className="update-body" data-cut={block.cut || undefined}>
        <Markdown {...hooks}>{block.text}</Markdown>
        {block.cut && <span className="update-cut">{'— cut off'}</span>}
      </div>
    </div>
  );
}

/** The turn's work, named by its length: on hover it sits beside Copy and opens the work block. */
export type WorkedLink = { label: string; onOpen: () => void };

export function AnswerBlock({ block, worked }: { block: Block<'answer'>; worked?: WorkedLink }) {
  const hooks = useAssetMarkdownHooks();
  if (!block.text) return null;
  return (
    <div className="answer-block" data-actions-host="">
      <Markdown {...hooks}>{block.text}</Markdown>
      {!block.streaming && (
        <RowActions className="answer-actions">
          <CopyButton text={block.text} label="Copy answer" size="message" iconSize="xs" />
          {worked && (
            <Button className="answer-worked" onClick={worked.onOpen}>
              {worked.label}
            </Button>
          )}
        </RowActions>
      )}
    </div>
  );
}

export function ErrorBlock({ block, onRetry }: { block: Block<'error'>; onRetry?: () => void }) {
  return <ErrorItem text={block.text} onRetry={onRetry} />;
}

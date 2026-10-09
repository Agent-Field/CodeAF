import { CopyButton, Markdown } from '../../../components/ui';
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
      <Markdown {...hooks}>{block.text}</Markdown>
      {block.cut && <span className="update-cut">{'— cut off'}</span>}
    </div>
  );
}

export function AnswerBlock({ block }: { block: Block<'answer'> }) {
  const hooks = useAssetMarkdownHooks();
  if (!block.text) return null;
  return (
    <div className="answer-block">
      <Markdown {...hooks}>{block.text}</Markdown>
      {!block.streaming && (
        <div className="answer-actions">
          <CopyButton text={block.text} label="Copy answer" />
        </div>
      )}
    </div>
  );
}

export function ErrorBlock({ block, onRetry }: { block: Block<'error'>; onRetry?: () => void }) {
  return <ErrorItem text={block.text} onRetry={onRetry} />;
}

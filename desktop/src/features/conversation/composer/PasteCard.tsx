import { IconButton, Text } from '../../../components/ui';
import './paste-card.css';

export type PasteCardProps = {
  lines: number;
  text: string;
  /** Present in the composer, where the person can take the paste back out; absent once sent. */
  onRemove?: () => void;
  variant?: 'composer' | 'sent';
};

/** Only the top of the paste shows, fading out; the whole paste is never drawn here. */
const PREVIEW_LINES = 3;

/** A long paste held as a card: its size, and a faded look at the top of it. */
export function PasteCard({ lines, text, onRemove, variant = 'composer' }: PasteCardProps) {
  const preview = text.split('\n').slice(0, PREVIEW_LINES).join('\n');
  return (
    <div className="paste-card" data-variant={variant}>
      <div className="paste-card-head">
        <Text className="paste-card-name">Pasted text</Text>
        <Text className="paste-card-lines">{lines} lines</Text>
        {onRemove && (
          <IconButton className="paste-card-remove" label="Remove pasted text" icon="close" iconSize="tiny" onClick={onRemove} />
        )}
      </div>
      <pre className="paste-card-preview" aria-hidden="true">{preview}</pre>
    </div>
  );
}

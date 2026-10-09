import { Button, Icon } from '../../components/ui';
import { EARLIER_KEY, earlierLabel } from './folding';
import './earlier-turns.css';

type EarlierRowProps = { count: number; open: boolean; onToggle: (id: string) => void };

/** "N earlier turns": one quiet row that opens the oldest turns, each already a folded line. */
export function EarlierRow({ count, open, onToggle }: EarlierRowProps) {
  return (
    <Button className="earlier-row" data-anchor={EARLIER_KEY} aria-expanded={open} onClick={() => onToggle(EARLIER_KEY)}>
      <span className="earlier-chevron" data-open={open || undefined} aria-hidden="true">
        <Icon name="chevronRight" size="xs" />
      </span>
      {earlierLabel(count)}
    </Button>
  );
}

/** The rule that tells the reader the lines above are digests, not the messages. */
export function SummaryDivider() {
  return (
    <div className="summary-divider" role="separator" aria-label="Earlier messages summarized">
      <span className="summary-divider-rule" aria-hidden="true" />
      Earlier messages summarized
      <span className="summary-divider-rule" aria-hidden="true" />
    </div>
  );
}

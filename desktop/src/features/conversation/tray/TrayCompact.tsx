import { Button } from '../../../components/ui';

type CompactProps = {
  /** Questions still standing, folded ones included. */
  count: number;
  /** The question the full tray would be showing, in plain words. */
  summary: string;
  onReview: () => void;
};

/** 1b: the 40px bar that stands in for the card while the reader is scrolled away. */
export function TrayCompact({ count, summary, onReview }: CompactProps) {
  return (
    <section className="tray-compact" aria-label="Questions waiting, summary">
      <span className="tray-mark" aria-hidden="true" />
      <span className="tray-compact-count">{`${count} need you`}</span>
      <span className="tray-compact-summary">{summary}</span>
      <Button className="tray-compact-review" onClick={onReview}>Review</Button>
    </section>
  );
}

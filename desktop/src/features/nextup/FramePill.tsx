import { Button } from '../../components/ui/Button';
import { nextUpChord } from './Banner';
import './frame-pill.css';

/** The text the pill reads aloud: the count, where they are, and the key (I2.1). */
export function framePillLabel(count: number, chord: string = nextUpChord): string {
  return `${count} need you elsewhere. Press ${chord}`;
}

/** Only a positive whole count draws a pill; zero and unknown are absence, not "0 need you". */
export function framePillVisible(count: number | undefined): count is number {
  return typeof count === 'number' && Number.isInteger(count) && count > 0;
}

/**
 * The frame pill: how many questions wait in conversations other than the one on
 * screen. Amber lives only on the 6px glyph; the words stay ink and muted so the
 * colour carries the meaning once. Click opens the queue, same as the chord.
 */
export function FramePill({ count, onOpen }: { count?: number; onOpen: () => void }) {
  if (!framePillVisible(count)) return null;
  return (
    <Button variant="ghost" className="frame-pill" aria-label={framePillLabel(count)} onClick={onOpen}>
      <span className="frame-pill-glyph" aria-hidden="true"/>
      <span className="frame-pill-count" aria-hidden="true">{count} need you</span>
      <span className="frame-pill-muted" aria-hidden="true">elsewhere</span>
      <span className="frame-pill-muted" aria-hidden="true">{nextUpChord}</span>
    </Button>
  );
}

/** The measurement page (`?specimen=frame-pill`). `count` picks the number; 0 draws nothing. */
export function FramePillSpecimen() {
  const count = Number(new URLSearchParams(window.location.search).get('count') ?? '5');
  return (
    <div className="frame-pill-specimen">
      <FramePill count={count} onOpen={() => { document.body.dataset.opened = 'queue'; }}/>
    </div>
  );
}

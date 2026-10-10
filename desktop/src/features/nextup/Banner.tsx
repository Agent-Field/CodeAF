import { useEffect, useRef, useState, type TransitionEvent } from 'react';
import { Button } from '../../components/ui/Button';
import { isMac } from '../../design/keyboard';
import design from '../../design/tokens.json';
import './banner.css';

/** How long a banner stays before it folds, read from the one token so the test and the card cannot drift. */
export const BANNER_HOLD_MS = Number.parseFloat(design.foundation['i2-banner-hold']);
const BANNER_ENTER_MS = Number.parseFloat(design.foundation['i2-banner-duration']);

/** One needs-you item the banner can show. The caller decides what "blocking" means. */
export type NextUpBannerItem = {
  id: string;
  blocking: boolean;
  /** The question, for example "Allow 3 git actions?". */
  head: string;
  /** Second line. Omitted when the engine did not send one; an empty string draws nothing. */
  detail?: string;
};

/** from-pill is the pose tucked 8px up into the pill. into-pill is the same pose on the way back. */
export type BannerPose = 'from-pill' | 'rest' | 'into-pill';

/** Blocking questions with a head only. Anything else is absent, not an empty card. */
export function bannerVisible(item: NextUpBannerItem | null): item is NextUpBannerItem {
  return item !== null && item.blocking && item.head.trim() !== '';
}

/** ⌘J on a Mac, Ctrl J elsewhere (P-5). Drawn tight on the Mac, as the specimen spells it. */
export const nextUpChord = isMac ? '⌘J' : 'Ctrl J';

type CardProps = {
  item: NextUpBannerItem;
  onOpen: (id: string) => void;
  /** Hold one pose for measurement. The live card leaves this unset and moves itself. */
  pose?: BannerPose;
};

/**
 * The 4s banner that slides out of the frame pill for one new blocking question.
 * Hover and keyboard focus pause the hold, because someone reading it should not
 * be raced. Click starts Next up at this item and folds the card. role=status is
 * announced when the card appears; aria-live turns off as it folds so the removal
 * is not a second announcement.
 */
function BannerCard({ item, onOpen, pose }: CardProps) {
  const [phase, setPhase] = useState<BannerPose>(pose ?? 'from-pill');
  const [present, setPresent] = useState(true);
  const [hovering, setHovering] = useState(false);
  const [focused, setFocused] = useState(false);
  const remaining = useRef(BANNER_HOLD_MS);
  const paused = hovering || focused;

  useEffect(() => {
    if (pose) return;
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => setPhase('rest'));
    });
    return () => { cancelAnimationFrame(first); cancelAnimationFrame(second); };
  }, [pose]);

  const folding = phase === 'into-pill';
  useEffect(() => {
    // The hold starts with the card, including the slide, and does not run while
    // it is paused or already folding. Settling from the pill into rest must not
    // restart it: that would bill the slide twice. A frozen pose is not a timer.
    if (!present || pose || paused || folding) return;
    const started = performance.now();
    const timer = window.setTimeout(() => setPhase('into-pill'), remaining.current);
    return () => {
      window.clearTimeout(timer);
      remaining.current = Math.max(0, remaining.current - (performance.now() - started));
    };
  }, [present, pose, paused, folding]);

  useEffect(() => {
    if (!present || pose || phase !== 'into-pill') return;
    // transitionend is the fast path. This covers a fold that starts before the
    // fade has a from-value to leave, which fires no event.
    const timer = window.setTimeout(() => setPresent(false), BANNER_ENTER_MS);
    return () => window.clearTimeout(timer);
  }, [present, pose, phase]);

  if (!present) return null;

  const open = () => {
    if (phase === 'into-pill') return;
    onOpen(item.id);
    if (!pose) setPhase('into-pill');
  };
  const onTransitionEnd = (event: TransitionEvent<HTMLButtonElement>) => {
    if (event.target !== event.currentTarget || event.propertyName !== 'opacity' || phase !== 'into-pill' || pose) return;
    setPresent(false);
  };

  return (
    <Button
      variant="ghost"
      className="nextup-banner"
      role="status"
      aria-live={phase === 'into-pill' ? 'off' : 'polite'}
      aria-atomic="true"
      data-phase={phase}
      onClick={open}
      onPointerEnter={() => setHovering(true)}
      onPointerLeave={() => setHovering(false)}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
      onTransitionEnd={onTransitionEnd}
    >
      <span className="nextup-banner-glyph" aria-hidden="true"/>
      <span className="nextup-banner-copy">
        <span className="nextup-banner-head">{item.head}</span>
        {item.detail ? <span className="nextup-banner-detail">{item.detail}</span> : null}
      </span>
      <span className="nextup-banner-chord">{nextUpChord}</span>
    </Button>
  );
}

/** One banner. A new id remounts the card so the hold, the slide and the announcement start over. */
export function Banner({ item, onOpen, pose }: { item: NextUpBannerItem | null; onOpen: (id: string) => void; pose?: BannerPose }) {
  if (!bannerVisible(item)) return null;
  return <BannerCard key={item.id} item={item} onOpen={onOpen} pose={pose}/>;
}

const example: NextUpBannerItem = {
  id: 'git-3',
  blocking: true,
  head: 'Allow 3 git actions?',
  detail: 'Config parser · holding up 2 tasks',
};

/** The measurement page (`?specimen=nextup-banner`). Not product chrome. */
export function BannerSpecimen() {
  const params = new URLSearchParams(window.location.search);
  const freeze = params.get('freeze');
  const pose: BannerPose | undefined = freeze === 'from-pill' || freeze === 'rest' || freeze === 'into-pill' ? freeze : undefined;
  const blocking = params.get('blocking') !== '0';
  const head = params.get('head') ?? example.head;
  const detail = params.get('detail') === '0' ? undefined : example.detail;
  const [item, setItem] = useState<NextUpBannerItem | null>(params.get('none') === '1' ? null : { ...example, blocking, head, detail });
  const [opened, setOpened] = useState('');
  return (
    <div className="nextup-banner-specimen" data-opened={opened || undefined}>
      <Banner item={item} pose={pose} onOpen={setOpened}/>
      <Button variant="quiet" onClick={() => setItem({ id: 'pricing', blocking: true, head: 'Which pricing line should lead?', detail: 'Marketing · Launch post' })}>Another blocking question</Button>
      <Button variant="ghost" onClick={() => setItem({ id: 'quiet', blocking: false, head: 'Port fix to v1 branch' })}>A running update</Button>
    </div>
  );
}

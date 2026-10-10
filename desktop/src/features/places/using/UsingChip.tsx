import type { Ref } from 'react';
import { Button, Icon, StatusMark } from '../../../components/ui';
import { PlaceSwatch } from '../components/PlaceSwatch';
import type { Tint } from '../client';
import type { UsingView } from '../using-types';
import './using.css';

export type UsingChipTone = 'quiet' | 'attention' | 'failed';

export type UsingChipProps = {
  text: string;
  tone: UsingChipTone;
  /** The screen-reader words for the amber mark that says a choice waits on the person. */
  mark?: string;
  /** One place and nothing else in use: the chip is the place's swatch and name (Places 9b) and carries no chevron. */
  place?: { name: string; tint: Tint };
  open: boolean;
  onToggle: () => void;
  buttonRef?: Ref<HTMLButtonElement>;
};

/**
 * The single place a chat is using, or undefined. Only a lone place with no sources collapses to its name (9b); with sources
 * the count line is the truer account, so it stays.
 */
export function singlePlace(view: UsingView | undefined): UsingChipProps['place'] {
  if (!view || view.bundle.counts.places !== 1 || view.bundle.counts.sources > 0) return undefined;
  const place = view.bundle.places[0];
  return place ? { name: place.name, tint: place.tint } : undefined;
}

/**
 * The Using chip (Places 6f, P-CMP-5): a layers glyph, "Using 3 places · 4 sources" and a chevron that turns up while the popover
 * is open. Enter and Space open it as on any button; hover, press and the focus ring come from the shared Button.
 */
export function UsingChip({ text, tone, mark, place, open, onToggle, buttonRef }: UsingChipProps) {
  const failed = tone === 'failed';
  const lone = place && !failed && !mark;
  return (
    <Button ref={buttonRef} className="using-chip" data-tone={tone} data-lone={lone || undefined} aria-expanded={open} aria-haspopup="dialog" onClick={onToggle}>
      {lone ? <PlaceSwatch tint={place.tint} role="card" /> : <Icon name={failed ? 'triangleAlert' : 'layers'} size="xs" />}
      <span className="using-chip-text">{lone ? place.name : text}</span>
      {mark && <StatusMark dense status="waiting" label={mark} />}
      {!lone && <Icon name={open ? 'chevronUp' : 'chevron'} size="xs" />}
    </Button>
  );
}

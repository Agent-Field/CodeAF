import { useCallback, useRef, useState } from 'react';
import { Button, Icon, StatusMark } from '../../components/ui';
import { UsingSheet } from './UsingSheet';
import { chipText, hasContext, settingFor } from './using-model';
import type { SourceHandoff, UsingView } from './using-types';
import type { UsingControl } from './useUsing';
import './using.css';

export type UsingLineProps = {
  control: UsingControl;
  /**
   * Opening a source is navigation, and navigation belongs to the shell. With no handler the sources are listed and
   * explained but carry no open control: a button that goes nowhere is never drawn.
   */
  onOpenSource?: (handoff: SourceHandoff) => void;
  /** Absent when this conversation cannot be filed in a place from here; the "Add to a place…" row is then left out. */
  onAddToPlace?: () => void;
  /** Whether the sheet starts open (the specimen and the tests). */
  startOpen?: boolean;
};

/** What the person's own call is waiting on: a pick between places, or a wider setting held back. */
function needsYou(view: UsingView | undefined): boolean {
  if (!view?.engine.places) return false;
  return (['model', 'permissions'] as const).some(field => {
    const state = settingFor(view, field)?.state;
    return state === 'needsPick' || state === 'needsYou';
  });
}

type Chip = { text: string; tone: 'quiet' | 'attention' | 'failed'; mark?: { words: string } };

/** The chip's words. Loading and a conversation with nothing to use say nothing; a failure always says so. */
function chipFor(control: UsingControl): Chip | undefined {
  const { state } = control;
  if (state.phase === 'absent' || state.phase === 'loading') return undefined;
  if (state.phase === 'failed') {
    if (state.view) return { text: chipText(state.view.bundle), tone: 'failed' };
    return { text: state.unreachable ? 'Places offline' : 'Places unavailable', tone: 'failed' };
  }
  const { view } = state;
  if (!view.engine.places) return view.bundle.counts.places > 0 ? { text: 'Places not read', tone: 'failed' } : undefined;
  if (!hasContext(view)) return undefined;
  return needsYou(view) ? { text: chipText(view.bundle), tone: 'attention', mark: { words: 'Needs your choice' } } : { text: chipText(view.bundle), tone: 'quiet' };
}

/** Whether the chip has anything to say; the header's owner uses this to decide if the bar is drawn at all. */
export const usingVisible = (control: UsingControl): boolean => chipFor(control) !== undefined;

/**
 * The Using chip in a conversation's header (Places 6e/6f): "Using 3 places · 4 sources", and a sheet listing every instruction,
 * source and policy under the place it came from. It draws nothing while loading and nothing when there is nothing to use.
 */
export function UsingLine({ control, onOpenSource, onAddToPlace, startOpen = false }: UsingLineProps) {
  const anchor = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(startOpen);
  const close = useCallback(() => { setOpen(false); anchor.current?.querySelector('button')?.focus(); }, []);
  const chip = chipFor(control);
  if (!chip) return null;
  const failed = chip.tone === 'failed';
  return (
    <span ref={anchor} className="using-anchor">
      <Button
        className="using-chip"
        data-tone={chip.tone}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => setOpen(value => !value)}
      >
        <Icon name={failed ? 'triangleAlert' : 'layers'} size="xs" />
        <span className="using-chip-text">{chip.text}</span>
        {chip.mark && <StatusMark dense status="waiting" label={chip.mark.words} />}
        <Icon name="chevronUp" size="xs" />
      </Button>
      {open && <UsingSheet control={control} anchor={anchor} onClose={close} onDismiss={() => setOpen(false)} onOpenSource={onOpenSource} onAddToPlace={onAddToPlace} />}
    </span>
  );
}

import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import { isMac } from '../../../design/keyboard';
import { ModelPopover, type EffortControl } from './ModelPopover';
import './model-picker.css';

export type ModelOption = {
  id: string;
  label: string;
  /** The chip's word for it ("Flash"); defaults to the last word of the label. */
  short?: string;
};

export type ModelPickerProps = {
  /** Only models the engine can run; today that is one. */
  models: readonly ModelOption[];
  selectedId: string;
  /** Absent when the engine exposes no routing: rows stay checked-only and ⌘1-3 do nothing. */
  onSelect?: (id: string) => void;
  /** Absent when the engine accepts no effort setting, so the Effort row does not render. */
  effort?: EffortControl;
};

/** Models pinned to the segmented control and to ⌘1-3. */
export const PINNED_LIMIT = 3;
const shortName = (model: ModelOption) => model.short ?? model.label.split(' ').pop() ?? model.label;
const isSwapKey = (event: KeyboardEvent) => (isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey) && !event.altKey && !event.shiftKey;

/**
 * The composer's model chip and its quick-swap popover (design 1e). ⌘1-3 switch the pinned
 * models directly and ⌘/ opens the list; a swap applies to the next message. Everything the
 * engine cannot do yet (more than one model, an effort setting) is absent, not disabled.
 */
export function ModelPicker({ models, selectedId, onSelect, effort }: ModelPickerProps) {
  const chip = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);
  const selected = models.find(model => model.id === selectedId);
  const close = useCallback(() => { setOpen(false); chip.current?.querySelector('button')?.focus(); }, []);
  const swap = useCallback((id: string) => { if (id !== selectedId) onSelect?.(id); }, [selectedId, onSelect]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!isSwapKey(event)) return;
      if (event.key === '/') { event.preventDefault(); setOpen(true); return; }
      const pinned = onSelect ? models.slice(0, PINNED_LIMIT)[Number(event.key) - 1] : undefined;
      if (!pinned) return;
      event.preventDefault();
      swap(pinned.id);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [models, onSelect, swap]);
  if (!selected) return null;
  return (
    <span ref={chip} className="model-picker-anchor">
      <Button
        className="model-picker"
        aria-label={`Model: ${selected.label}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        data-state={open ? 'open' : 'closed'}
        onClick={() => setOpen(value => !value)}
      >
        <span className="model-picker-label">{shortName(selected)}</span>
        <Icon name="chevron" size="xs" />
      </Button>
      {open && (
        <ModelPopover
          anchor={chip}
          models={models}
          pinnedCount={PINNED_LIMIT}
          shortName={shortName}
          selectedId={selectedId}
          canSwap={Boolean(onSelect)}
          effort={effort}
          onSwap={id => { swap(id); close(); }}
          onClose={close}
          onDismiss={() => setOpen(false)}
        />
      )}
    </span>
  );
}

import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import { isMac } from '../../../design/keyboard';
import { ModelPopover, type EffortControl } from './ModelPopover';
import { PINNED_LIMIT } from './modelOrder';
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
  /** How many leading models are pinned segments and take ⌘1-3; three unless the list says fewer. */
  pinnedCount?: number;
};

export { PINNED_LIMIT };
const shortName = (model: ModelOption) => model.short ?? model.label.split(' ').pop() ?? model.label;
const isSwapKey = (event: KeyboardEvent) => (isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey) && !event.altKey && !event.shiftKey;

/**
 * The composer's model chip and its quick-swap popover (design 1e). ⌘1-3 switch the pinned
 * models directly and ⌘/ opens the list; a swap applies to the next message. Everything the
 * engine cannot do yet (more than one model, an effort setting) is absent, not disabled.
 */
export function ModelPicker({ models, selectedId, onSelect, effort, pinnedCount = PINNED_LIMIT }: ModelPickerProps) {
  const chip = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);
  const selected = models.find(model => model.id === selectedId);
  const close = useCallback(() => { setOpen(false); chip.current?.querySelector('button')?.focus(); }, []);
  const swap = useCallback((id: string) => { if (id !== selectedId) onSelect?.(id); }, [selectedId, onSelect]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!isSwapKey(event)) return;
      if (event.key === '/') { event.preventDefault(); setOpen(true); return; }
      const pinned = onSelect ? models.slice(0, pinnedCount)[Number(event.key) - 1] : undefined;
      if (!pinned) return;
      event.preventDefault();
      // The design gives ⌘1-3 to the pinned models; the workspace's tab-by-number keys must not also act on them.
      event.stopPropagation();
      swap(pinned.id);
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [models, onSelect, swap, pinnedCount]);
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
          pinnedCount={pinnedCount}
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

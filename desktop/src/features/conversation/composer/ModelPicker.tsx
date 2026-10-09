import { useCallback, useRef, useState } from 'react';
import { Button, Icon } from '../../../components/ui';
import { shortcutLayer } from '../../../design/keyboard';
import { useShortcuts } from '../../../design/useShortcuts';
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
  /** Absent when the engine exposes no routing: rows stay checked-only and ⌥⌘1-3 do nothing. */
  onSelect?: (id: string) => void;
  /** Absent when the engine accepts no effort setting, so the Effort row does not render. */
  effort?: EffortControl;
  /** How many leading models are pinned segments and take ⌥⌘1-3; three unless the list says fewer. */
  pinnedCount?: number;
};

export { PINNED_LIMIT };
const shortName = (model: ModelOption) => model.short ?? model.label.split(' ').pop() ?? model.label;

/**
 * The composer's model chip and its quick-swap popover (design 1e). ⌥⌘1-3 switch the pinned
 * models directly and ⌘/ opens the list; a swap applies to the next message. Everything the
 * engine cannot do yet (more than one model, an effort setting) is absent, not disabled.
 */
export function ModelPicker({ models, selectedId, onSelect, effort, pinnedCount = PINNED_LIMIT }: ModelPickerProps) {
  const chip = useRef<HTMLSpanElement>(null);
  const [open, setOpen] = useState(false);
  const selected = models.find(model => model.id === selectedId);
  const close = useCallback(() => { setOpen(false); chip.current?.querySelector('button')?.focus(); }, []);
  const swap = useCallback((id: string) => { if (id !== selectedId) onSelect?.(id); }, [selectedId, onSelect]);
  // The shell's one shortcut registry (design/keyboard.ts): this surface is on screen, so it sees ⌥⌘1-3 first.
  // ⌘1-9 are the tab jump (design Interactions) and never reach here; a slot with no pinned model returns false.
  useShortcuts(shortcutLayer.surface, shortcut => {
    if (shortcut.id === 'models') { setOpen(true); return true; }
    const pinned = shortcut.id === 'model-pin' && onSelect ? models.slice(0, pinnedCount)[shortcut.index! - 1] : undefined;
    if (!pinned) return false;
    swap(pinned.id);
    return true;
  });
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

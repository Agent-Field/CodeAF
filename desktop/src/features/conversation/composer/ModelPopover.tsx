import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { Button, Icon, KeyboardShortcut } from '../../../components/ui';
import { isMac } from '../../../design/keyboard';
import design from '../../../design/tokens.json';
import type { ModelOption } from './ModelPicker';

export type EffortControl = {
  value: string;
  options: readonly { value: string; label: string }[];
  onChange: (value: string) => void;
};

type ModelPopoverProps = {
  anchor: RefObject<HTMLElement | null>;
  models: readonly ModelOption[];
  pinnedCount: number;
  shortName: (model: ModelOption) => string;
  selectedId: string;
  /** False while the engine cannot swap: rows are shown checked-only and carry no shortcut. */
  canSwap: boolean;
  effort?: EffortControl;
  /** A model was picked: swap, then close and return focus to the chip. */
  onSwap: (id: string) => void;
  /** Esc or Tab: close and return focus to the chip. */
  onClose: () => void;
  /** Pointer outside or the anchor scrolled: close without stealing focus. */
  onDismiss: () => void;
};

const NAV = '[data-nav]:not(:disabled)';
const chord = (key: string) => (isMac ? `⌘${key}` : `Ctrl ${key}`);

/** Keeps the popover above its chip, inside the window, in two custom properties the stylesheet reads. */
function usePlacement(anchor: RefObject<HTMLElement | null>, popover: RefObject<HTMLDivElement | null>) {
  useLayoutEffect(() => {
    const chip = anchor.current?.getBoundingClientRect();
    const node = popover.current;
    if (!chip || !node) return;
    const pad = design.overlay.collisionPadding;
    const left = Math.min(Math.max(chip.left, pad), Math.max(pad, window.innerWidth - node.offsetWidth - pad));
    node.style.setProperty('--model-popover-left', `${Math.round(left)}px`);
    node.style.setProperty('--model-popover-bottom', `${Math.round(window.innerHeight - chip.top + design.overlay.sideOffset)}px`);
  }, [anchor, popover]);
}

/** Popovers close when their anchor scrolls (1f), when the window resizes, or on a press outside. */
function useDismiss(popover: RefObject<HTMLDivElement | null>, anchor: RefObject<HTMLElement | null>, onDismiss: () => void) {
  useEffect(() => {
    const outside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!popover.current?.contains(target) && !anchor.current?.contains(target)) onDismiss();
    };
    const scrolled = (event: Event) => { if (!(event.target instanceof Node && popover.current?.contains(event.target))) onDismiss(); };
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('scroll', scrolled, true);
    window.addEventListener('resize', onDismiss);
    return () => {
      document.removeEventListener('pointerdown', outside, true);
      window.removeEventListener('scroll', scrolled, true);
      window.removeEventListener('resize', onDismiss);
    };
  }, [popover, anchor, onDismiss]);
}

function PinnedSegments({ pinned, selectedId, shortName, onPick }: { pinned: readonly ModelOption[]; selectedId: string; shortName: (m: ModelOption) => string; onPick: (id: string) => void }) {
  return (
    <div className="model-popover-pinned" role="radiogroup" aria-label="Pinned models">
      {pinned.map(model => (
        <Button key={model.id} role="radio" data-nav aria-checked={model.id === selectedId} className="model-popover-segment" onClick={() => onPick(model.id)}>
          {shortName(model)}
        </Button>
      ))}
    </div>
  );
}

function EffortRow({ effort }: { effort: EffortControl }) {
  return (
    <div className="model-popover-effort">
      <span id="model-effort-label">Effort</span>
      <div role="radiogroup" aria-labelledby="model-effort-label" className="model-popover-effort-options">
        {effort.options.map(option => (
          <Button key={option.value} role="radio" data-nav aria-checked={option.value === effort.value} className="model-popover-effort-option" onClick={() => effort.onChange(option.value)}>
            {option.label}
          </Button>
        ))}
      </div>
    </div>
  );
}

function ModelRow({ model, checked, shortcut, onPick }: { model: ModelOption; checked: boolean; shortcut?: string; onPick: () => void }) {
  return (
    <Button role="radio" data-nav aria-checked={checked} className="model-popover-row" onClick={onPick}>
      <span className="model-popover-mark">{checked && <Icon name="check" size="xs" />}</span>
      <span className="model-popover-name">{model.label}</span>
      {shortcut && <KeyboardShortcut label={shortcut} />}
    </Button>
  );
}

/** Arrow keys move through every control, Home and End jump, Esc and Tab close with focus back on the chip. */
function navigate(event: KeyboardEvent<HTMLDivElement>, onClose: () => void) {
  if (event.key === 'Escape' || event.key === 'Tab') { event.preventDefault(); event.stopPropagation(); onClose(); return; }
  const step = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 }[event.key];
  const edge = { Home: 0, End: -1 }[event.key];
  if (step === undefined && edge === undefined) return;
  event.preventDefault();
  const stops = [...event.currentTarget.querySelectorAll<HTMLElement>(NAV)];
  const at = stops.indexOf(document.activeElement as HTMLElement);
  const next = step === undefined ? (edge === 0 ? 0 : stops.length - 1) : (at + step + stops.length) % stops.length;
  stops[next]?.focus();
}

/**
 * The design's quick-swap popover (1e): pinned segments, an Effort row when the engine takes
 * one, the model list with ⌘1-3, and "All models…  ⌘/" when more models exist than are pinned.
 */
export function ModelPopover({ anchor, models, pinnedCount, shortName, selectedId, canSwap, effort, onSwap, onClose, onDismiss }: ModelPopoverProps) {
  const popover = useRef<HTMLDivElement>(null);
  const [all, setAll] = useState(false);
  const pinned = models.slice(0, pinnedCount);
  // A model in use that is not pinned stays on the list, checked, so the popover never hides the current choice.
  const current = models.find(model => model.id === selectedId);
  const listed = all ? models : current && !pinned.includes(current) ? [...pinned, current] : pinned;
  const hasMore = models.length > listed.length;
  usePlacement(anchor, popover);
  useDismiss(popover, anchor, onDismiss);
  useEffect(() => {
    const target = popover.current?.querySelector<HTMLElement>('.model-popover-row[aria-checked="true"]') ?? popover.current?.querySelector<HTMLElement>(NAV);
    target?.focus();
  }, []);
  const pick = (id: string) => (canSwap ? onSwap(id) : onClose());
  return createPortal(
    <div ref={popover} className="model-popover" role="dialog" aria-label="Model" onKeyDown={event => navigate(event, onClose)}>
      <PinnedSegments pinned={pinned} selectedId={selectedId} shortName={shortName} onPick={pick} />
      {effort && <EffortRow effort={effort} />}
      <div className="model-popover-rule" />
      <div role="radiogroup" aria-label="Models" className="model-popover-list">
        {listed.map((model, index) => (
          <ModelRow key={model.id} model={model} checked={model.id === selectedId} shortcut={canSwap && index < pinnedCount ? chord(String(index + 1)) : undefined} onPick={() => pick(model.id)} />
        ))}
      </div>
      {hasMore && !all && (
        <Button data-nav className="model-popover-row model-popover-all" onClick={() => setAll(true)}>
          <span className="model-popover-mark"><Icon name="search" size="xs" /></span>
          <span className="model-popover-name">All models…</span>
          <KeyboardShortcut label={chord('/')} />
        </Button>
      )}
    </div>,
    document.body,
  );
}

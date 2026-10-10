import { useEffect, useId, useRef, type DragEvent, type HTMLAttributes, type KeyboardEvent, type MouseEvent } from 'react';
import { Button, ContextMenu, Icon, StatusMark, TextInput, type MenuEntry } from '../../../components/ui';
import { PlaceSwatch, choosableTints, tintLabel, type TintName } from './PlaceSwatch';
import './places-components.css';

/** Where a tint came from (9d "Tint marks a family"): chosen on this place, or followed from its first parent. Same look; carried as data. */
export type TintSource = 'own' | 'inherited';
/** The dot at a tile's top right: a child needs you (amber) or failed (red). Running never shows on a tile (10a). */
export type TileStatus = 'waiting' | 'failed';
const statusWords: Record<TileStatus, string> = { waiting: 'Needs you', failed: 'Failed' };

type Frame = Omit<HTMLAttributes<HTMLLIElement>, 'title' | 'onClick' | 'onKeyDown' | 'onDrop' | 'onDragStart' | 'onDragEnd' | 'onDragOver' | 'onDragEnter' | 'onDragLeave'>;
type CommonProps = Frame & {
  disabled?: boolean;
  /** Forces a look, for specimens only. */
  appearance?: 'hover' | 'pressed' | 'focus';
};

type PlaceProps = CommonProps & {
  mode?: 'place';
  /** The canonical place id. Rides on the tile (data-place-id) and closes over every callback. */
  id: string;
  name: string;
  /** The effective tint: the place's own, or the one inherited from its first parent. */
  tint: TintName;
  tintSource?: TintSource;
  /** "28 chats", "6 chats · also in Software", "47 places · 212 chats". The caller counts; the tile never invents one. */
  meta?: string;
  status?: TileStatus;
  statusLabel?: string;
  /** Select for Quick Look: the 2px accent ring. */
  selected?: boolean;
  /** Something is being dragged over this tile and would be added to it: the soft fill, 1.5px ring and "Add here". */
  dropTarget?: boolean;
  dropLabel?: string;
  /** This tile is being dragged (to nest it or to pin it). */
  dragging?: boolean;
  /** Enter or click: Go to. */
  onGoTo: () => void;
  /** Command or Control click, Command or Control Enter, or middle click. */
  onOpenInNewWindow?: () => void;
  /** Space. Only when given; otherwise Space is an ordinary press. */
  onQuickLook?: () => void;
  menu?: readonly MenuEntry[];
  menuLabel?: string;
  draggable?: boolean;
  onDragStart?: (event: DragEvent<HTMLLIElement>) => void;
  onDragEnd?: (event: DragEvent<HTMLLIElement>) => void;
  onDragEnter?: (event: DragEvent<HTMLLIElement>) => void;
  onDragOver?: (event: DragEvent<HTMLLIElement>) => void;
  onDragLeave?: (event: DragEvent<HTMLLIElement>) => void;
  onDrop?: (event: DragEvent<HTMLLIElement>) => void;
  onKeyDown?: (event: KeyboardEvent<HTMLButtonElement>) => void;
};
type NewProps = CommonProps & {
  mode: 'new';
  label?: string;
  onCreate: () => void;
  onKeyDown?: (event: KeyboardEvent<HTMLButtonElement>) => void;
};
type CreatingProps = CommonProps & {
  mode: 'creating';
  name: string;
  tint: TintName;
  onNameChange: (name: string) => void;
  onTintChange: (tint: TintName) => void;
  /** Enter, once the name has something in it. */
  onSubmit: () => void;
  /** Escape. */
  onCancel: () => void;
  /** The caller's unique-name check failed: the field says so to a screen reader. */
  invalid?: boolean;
  hint?: string;
};
export type PlaceTileProps = PlaceProps | NewProps | CreatingProps;

/** Plain activation keys. Space is Quick Look when asked for; Command or Control Enter opens a new window. */
function tileKeys(props: PlaceProps) {
  return (event: KeyboardEvent<HTMLButtonElement>) => {
    props.onKeyDown?.(event);
    if (event.defaultPrevented) return;
    if (event.key === ' ' && props.onQuickLook) { event.preventDefault(); props.onQuickLook(); }
    else if (event.key === 'Enter' && (event.metaKey || event.ctrlKey) && props.onOpenInNewWindow) { event.preventDefault(); props.onOpenInNewWindow(); }
  };
}

function PlaceTileBody(props: PlaceProps) {
  const { id, name, tint, tintSource, meta, status, statusLabel, selected, dropTarget, dropLabel = 'Add here', dragging, disabled, appearance, onGoTo, onOpenInNewWindow,
    menu, menuLabel, draggable, onDragStart, onDragEnd, onDragEnter, onDragOver, onDragLeave, onDrop, className = '', ...rest } = props;
  // Keys, Quick Look and the mode are handled by tileKeys and the dispatcher, not by the frame.
  const { onKeyDown: _keys, onQuickLook: _look, mode: _mode, ...frame } = rest;
  const open = (event: MouseEvent<HTMLButtonElement>) => {
    if ((event.metaKey || event.ctrlKey) && onOpenInNewWindow) onOpenInNewWindow(); else onGoTo();
  };
  const tile = <li {...frame} className={`places-tile ${className}`} data-mode="place" data-place-id={id} data-tint-name={tint} data-tint-source={tintSource}
    data-selected={selected || undefined} data-drop={dropTarget || undefined} data-dragging={dragging || undefined} data-disabled={disabled || undefined} data-force={appearance}
    draggable={draggable && !disabled} onDragStart={onDragStart} onDragEnd={onDragEnd} onDragEnter={onDragEnter} onDragOver={onDragOver} onDragLeave={onDragLeave} onDrop={onDrop}>
    <Button variant="ghost" className="places-tile-main" data-places-tile-focusable disabled={disabled} aria-current={selected ? 'true' : undefined} data-force={appearance}
      onClick={open} onKeyDown={tileKeys(props)} onAuxClick={event => { if (event.button === 1 && onOpenInNewWindow) { event.preventDefault(); onOpenInNewWindow(); } }}>
      <span className="places-tile-head">
        <PlaceSwatch tint={tint} role="tile"/>
        {status && <span className="places-tile-status"><StatusMark status={status} label={statusLabel ?? statusWords[status]} dense/></span>}
      </span>
      <span className="places-tile-name">{name}</span>
      {meta && <span className="places-tile-meta">{meta}</span>}
      {dropTarget && <span className="places-tile-drop">{dropLabel}</span>}
    </Button>
  </li>;
  return menu && menu.length > 0 ? <ContextMenu items={menu} label={menuLabel ?? `${name} actions`}>{tile}</ContextMenu> : tile;
}

function NewPlaceTile({ label = 'New place', onCreate, onKeyDown, disabled, appearance, className = '', ...frame }: NewProps) {
  return <li {...frame} className={`places-tile ${className}`} data-mode="new" data-disabled={disabled || undefined} data-force={appearance}>
    <Button variant="ghost" className="places-tile-main" data-places-tile-focusable disabled={disabled} data-force={appearance} onClick={onCreate} onKeyDown={onKeyDown}>
      <Icon name="plus" size="md"/>
      <span className="places-tile-new-label">{label}</span>
    </Button>
  </li>;
}

/** Inline create (8f): the tile becomes a field that takes a name and a tint. It owns focus while it is open. */
function CreatingTile({ name, tint, onNameChange, onTintChange, onSubmit, onCancel, invalid, hint = '↵ create · Esc cancel', disabled, appearance, className = '', ...frame }: CreatingProps) {
  const field = useRef<HTMLInputElement>(null);
  const hintId = useId();
  const swatches = useRef<HTMLDivElement>(null);
  useEffect(() => { field.current?.focus(); }, []);
  const pick = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const next = choosableTints[(index + step + choosableTints.length) % choosableTints.length];
    onTintChange(next);
    swatches.current?.querySelector<HTMLElement>(`[data-tint-name="${next}"]`)?.closest('button')?.focus();
  };
  return <li {...frame} className={`places-tile ${className}`} data-mode="creating" data-disabled={disabled || undefined} data-force={appearance}>
    <div className="places-tile-form">
      <div ref={swatches} role="radiogroup" aria-label="Tint" className="places-tile-choices">
        {choosableTints.map((choice, index) => <Button key={choice} variant="ghost" role="radio" aria-checked={choice === tint} aria-label={tintLabel[choice]} className="places-tile-choice"
          tabIndex={choice === tint || (!choosableTints.includes(tint) && index === 0) ? 0 : -1} disabled={disabled} onClick={() => onTintChange(choice)} onKeyDown={event => pick(event, index)}>
          <PlaceSwatch tint={choice} role="choice" selected={choice === tint} dimmed={choice !== tint}/>
        </Button>)}
      </div>
      <TextInput className="places-tile-field" ref={field} value={name} disabled={disabled} aria-label="Place name" aria-invalid={invalid || undefined} aria-describedby={hintId} autoComplete="off" spellCheck={false}
        onChange={event => onNameChange(event.target.value)}
        onKeyDown={event => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === 'Enter' && name.trim()) { event.preventDefault(); onSubmit(); }
          else if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onCancel(); }
        }}/>
      <span id={hintId} className="places-tile-hint">{hint}</span>
    </div>
  </li>;
}

/** One tile of a place's Places grid (Components "Place tile"): rest, hover, pressed, focus, selected, drop target, dragging, disabled,
 * plus the quiet "New place" tile and the inline-create tile. */
export function PlaceTile(props: PlaceTileProps) {
  if (props.mode === 'new') return <NewPlaceTile {...props}/>;
  if (props.mode === 'creating') return <CreatingTile {...props}/>;
  return <PlaceTileBody {...props}/>;
}

export { PlaceGrid as PlaceTileGrid } from '../home/PlaceGrid';

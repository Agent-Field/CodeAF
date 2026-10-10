import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { Button, Icon, Tag } from '../../../components/ui';
import design from '../../../design/tokens.json';
import { PlaceSwatch } from '../components/PlaceSwatch';
import { PolicyRow } from '../PolicyConflict';
import { SourceList } from '../UsingSources';
import { decisionFor, instructionRows, settingFor, sourceTally, trimmedInstructions, type InstructionRow } from '../using-model';
import type { PolicyField, SourceHandoff, UsingBundle, UsingView } from '../using-types';
import type { UsingControl } from '../useUsing';
import '../using.css';
import { usePlacesShell } from '../shell/PlacesShell';
import { pickPlace } from '../palette/pickPlace';

export type UsingPopoverProps = {
  control: UsingControl;
  anchor: RefObject<HTMLElement | null>;
  /** Escape: close and give focus back to the chip. */
  onClose: () => void;
  /** A press outside, or the window resizing: close without taking focus. */
  onDismiss: () => void;
  onOpenSource?: (handoff: SourceHandoff) => void;
  onAddToPlace?: () => void;
  /** Files stay in this chat’s attachment tray until the next message sends them. */
  onDropFiles?: (files: File[]) => void;
};

/** Keeps the sheet under its chip and inside the window, in two custom properties the stylesheet reads. */
function usePlacement(anchor: RefObject<HTMLElement | null>, sheet: RefObject<HTMLDivElement | null>) {
  useLayoutEffect(() => {
    const chip = anchor.current?.getBoundingClientRect();
    const node = sheet.current;
    if (!chip || !node) return;
    if (window.innerWidth <= design.breakpoints.small) return;
    const pad = design.overlay.collisionPadding;
    const left = Math.min(Math.max(chip.left, pad), Math.max(pad, window.innerWidth - node.offsetWidth - pad));
    node.style.setProperty('--using-sheet-left', `${Math.round(left)}px`);
    node.style.setProperty('--using-sheet-top', `${Math.round(Math.max(pad, Math.min(chip.bottom + design.overlay.sideOffset, window.innerHeight - node.offsetHeight - pad)))}px`);
  }, [anchor, sheet]);
}

/** A press outside or a resize ends the sheet. Scrolling inside it never does: the list can be longer than the window. */
function useDismiss(sheet: RefObject<HTMLDivElement | null>, anchor: RefObject<HTMLElement | null>, onDismiss: () => void) {
  useEffect(() => {
    const outside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!sheet.current?.contains(target) && !anchor.current?.contains(target)) onDismiss();
    };
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('resize', onDismiss);
    return () => {
      document.removeEventListener('pointerdown', outside, true);
      window.removeEventListener('resize', onDismiss);
    };
  }, [sheet, anchor, onDismiss]);
}

function Section({ label, note, children }: { label: string; note?: string; children: ReactNode }) {
  return (
    <section className="using-section" aria-label={label}>
      <h3 className="using-label">{label}</h3>
      {note && <p className="using-note">{note}</p>}
      {children}
    </section>
  );
}

function Places({ bundle }: { bundle: UsingBundle }) {
  return (
    <ul className="using-places" aria-label="Places this conversation belongs to">
      {bundle.places.map(place => (
        <li key={place.id} className="using-place" data-inherited={place.inherited || undefined}>
          {!place.inherited && <PlaceSwatch tint={place.tint} role="card" />}
          <span>{place.name}</span>{' '}
          {place.inherited && <span className="using-place-level">· inherited</span>}
        </li>
      ))}
    </ul>
  );
}

function InstructionRow({ row }: { row: InstructionRow }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <li className="using-row" data-kind="instruction" data-replaced={row.struck || undefined}>
      <Button className="using-row-main" aria-expanded={expanded} onClick={() => setExpanded(value => !value)}>
        <Icon name="textLines" size="xs" />
        <span className="using-row-copy">
          <span className="using-row-text" data-expanded={expanded || undefined} data-replaced={row.struck || undefined}>{row.text}</span>
          {row.source && <span className="using-row-source">{row.source}</span>}
        </span>
        <span className="using-row-aside">{row.places}</span>
      </Button>
      {row.trimmed && <Tag className="using-row-tag" title="Only the start of this reached the model, to stay within the instruction limit.">Cut short</Tag>}
    </li>
  );
}

function Instructions({ bundle }: { bundle: UsingBundle }) {
  const cut = trimmedInstructions(bundle);
  const rows = instructionRows(bundle);
  if (rows.length === 0) return null;
  return (
    <Section label="Instructions" note={cut > 0 ? `${cut} cut short to fit the instruction limit; the model reads the rest.` : undefined}>
      <ul className="using-list">
        {rows.map(row => <InstructionRow key={row.key} row={row} />)}
      </ul>
    </Section>
  );
}

const fields: readonly PolicyField[] = ['model', 'permissions'];

function Policy({ control, view }: { control: UsingControl; view: UsingView }) {
  const rows = fields.filter(field => decisionFor(view.bundle, field) || settingFor(view, field));
  if (rows.length === 0) return null;
  return (
    <Section label="Policy">
      <ul className="using-list">
        {rows.map(field => (
          <PolicyRow key={field} field={field} bundle={view.bundle} decision={decisionFor(view.bundle, field)} setting={settingFor(view, field)} busy={control.busy === field} onChoose={placeId => control.choose(field, placeId)} onApply={() => control.apply(field)} />
        ))}
      </ul>
    </Section>
  );
}

/** A failure stays on screen with its own sentence and a way to try again; it is never dismissed by the sheet closing and reopening. */
function Failure({ message, unreachable, onRetry }: { message: string; unreachable: boolean; onRetry: () => void }) {
  return (
    <div className="using-alert" role="alert" data-tone="failed">
      <Icon name="triangleAlert" size="sm" />
      <span className="using-alert-text">{unreachable ? 'The engine is not reachable. ' : ''}{message}</span>
      <Button variant="quiet" className="using-alert-action" onClick={onRetry}><Icon name="retry" size="xs" />Try again</Button>
    </div>
  );
}

function Body({ control, view, onOpenSource }: { control: UsingControl; view: UsingView; onOpenSource?: (handoff: SourceHandoff) => void }) {
  const { bundle } = view;
  const reads = view.engine.places;
  const { refused, missing } = sourceTally(bundle);
  const tally = [refused > 0 ? `${refused} refused` : undefined, missing > 0 ? `${missing} not found` : undefined].filter(Boolean).join(' · ');
  const hasSources = bundle.sources.length + bundle.trimmed.length + bundle.refused.length > 0;
  return (
    <>
      {!reads && (
        <div className="using-alert" role="status" data-tone="quiet">
          <Icon name="info" size="sm" />
          <span className="using-alert-text">{view.engine.reason ?? 'This conversation’s engine does not read places, so none of this reached it.'}</span>
        </div>
      )}
      {bundle.places.length > 0 && <Section label="Places"><Places bundle={bundle} /></Section>}
      {reads && <Instructions bundle={bundle} />}
      {reads && hasSources && (
        <Section label="Sources" note={tally}>
          <SourceList bundle={bundle} onOpen={onOpenSource} />
          {bundle.trimmed.length > 0 && <p className="using-note using-trimmed">{bundle.trimmed.length} {bundle.trimmed.length === 1 ? 'source' : 'sources'} trimmed</p>}
        </Section>
      )}
      {reads && <Policy control={control} view={view} />}
    </>
  );
}

/**
 * The Using sheet (Places 6f, I2.13): Places, Instructions, Sources and Policy. Instructions lists each knows line, labelled with its place and whether it was learned or replaced. A
 * non-modal dialog under the chip: Escape closes it and returns focus to the chip, a press outside closes it, and Tab moves
 * through its real controls.
 */
export function UsingPopover({ control, anchor, onClose, onDismiss, onOpenSource, onAddToPlace, onDropFiles }: UsingPopoverProps) {
  const shell = usePlacesShell();
  const sheet = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState(false);
  usePlacement(anchor, sheet);
  useDismiss(sheet, anchor, onDismiss);
  useEffect(() => { sheet.current?.focus({ preventScroll: true }); }, []);
  const { state } = control;
  const view = state.phase === 'ready' || state.phase === 'failed' ? state.view : undefined;
  const addToPlace = () => {
    onDismiss();
    if (!shell || !view) { onAddToPlace?.(); return; }
    // Picking adds membership without moving the chat out of any place it already belongs to.
    void pickPlace({ title: 'Add to a place', exclude: view.bundle.places.filter(place => !place.inherited).map(place => place.id) }).then(async id => {
      if (!id) return;
      await shell.write('Added this conversation to a place', () => shell.client.addChats(id, [view.chatId]));
      control.reload();
    }).catch(shell.warn);
  };
  const keys = (event: KeyboardEvent) => {
    if (event.key !== 'Escape' || event.defaultPrevented) return;
    event.preventDefault();
    event.stopPropagation();
    onClose();
  };
  return createPortal(
    <div ref={sheet} className="using-sheet" role="dialog" aria-label="What this conversation is using" tabIndex={-1} onKeyDown={keys} data-testid="using-sheet" data-drop={dragging || undefined}
      onDragOver={event => {
        if (!onDropFiles || !Array.from(event.dataTransfer.types).includes('Files')) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = 'copy';
        setDragging(true);
      }}
      onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragging(false); }}
      onDrop={event => {
        if (!onDropFiles || !Array.from(event.dataTransfer.types).includes('Files')) return;
        event.preventDefault();
        setDragging(false);
        onDropFiles(Array.from(event.dataTransfer.files));
      }}>
      {state.phase === 'failed' && <Failure message={state.message} unreachable={state.unreachable} onRetry={control.reload} />}
      {control.writeError && (
        <div className="using-alert" role="alert" data-tone="failed">
          <Icon name="triangleAlert" size="sm" />
          <span className="using-alert-text">{control.writeError}</span>
          <Button variant="quiet" className="using-alert-action" onClick={control.dismissWriteError}>Dismiss</Button>
        </div>
      )}
      {view && <Body control={control} view={view} onOpenSource={onOpenSource} />}
      {(onAddToPlace || (shell && view)) && (
        <div className="using-foot">
          <Button className="using-add" onClick={addToPlace}><Icon name="plus" size="xs" />Add to a place…</Button>
        </div>
      )}
    </div>,
    document.body,
  );
}

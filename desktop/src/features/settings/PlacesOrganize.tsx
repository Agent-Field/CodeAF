// Organize now, in Settings under Places organization. The look is not in the
// design files (PO-1, PO-2, PO-3). The sentences come from placesOrganizeModel.
// A 404 means this engine has no organizing run, so the block is absent rather
// than a button that cannot work.
import { useEffect, useId, useRef, useState, useSyncExternalStore } from 'react';
import { Button, Icon, SectionHeading, Segmented, StatusMark, Text } from '../../components/ui';
import { worldStore } from '../chat/world-store.ts';
import { useWorld } from '../world/useWorld.ts';
import type { WorldClient } from '../world/worldClient.ts';
import { OrganizeError, organizeClient } from './placesOrganizeClient.ts';
import {
  ORGANIZE_MODES, initialIncluded, organizeFixtures, organizeScreen, screenOf, skipOf,
  type OrganizeMode, type OrganizeProgress, type OrganizeScreen, type OrganizeSnapshot,
} from './placesOrganizeModel.ts';
import '../../styles/tokens.css';
import '../../styles/ui.css';
import './placesOrganize.css';

const selectOrganizing = (world: WorldClient) => world.organizing();
const selectStoredOrganizing = () => worldStore.getState().organizing;

/** The world-feed record wins; the chat store is the same event when that client has not applied it yet. */
function preferFeed(primary?: OrganizeProgress, secondary?: OrganizeProgress): OrganizeProgress | undefined {
  return primary ?? secondary;
}

function asFeed(value: { run: string; stage: string; done: number; total: number; cancellable: boolean } | undefined): OrganizeProgress | undefined {
  if (!value?.run) return undefined;
  return { run: value.run, stage: value.stage, done: value.done, total: value.total, cancellable: value.cancellable };
}

// The accept's receipt stays for this app session so leaving Settings and coming
// back still offers Undo. It is not written to storage.
let heldReceipt = '';

type ViewProps = {
  screen: OrganizeScreen;
  mode: OrganizeMode;
  onMode: (mode: OrganizeMode) => void;
  included: ReadonlySet<number>;
  onToggle: (index: number) => void;
  sheetOpen: boolean;
  onOrganize: () => void;
  onCancel: () => void;
  onReview: () => void;
  onAccept: () => void;
  onDecline: () => void;
  onDismiss: () => void;
  onUndo: () => void;
  undo: boolean;
  busy: boolean;
  error: string;
};

function AlertLine({ text }: { text: string }) {
  return <p className="places-organize-error" role="alert"><span aria-hidden="true"><StatusMark status="failed" label="Failed" dense /></span><span>{text}</span></p>;
}

/** The drawing. Every sentence is already on the screen; this does not ask the engine. */
export function PlacesOrganizeView(props: ViewProps) {
  const { screen } = props;
  const dialog = useRef<HTMLDialogElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const firstRow = useRef<HTMLButtonElement>(null);
  const acceptButton = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (props.sheetOpen && !element.open) {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      element.showModal();
      // showModal runs after React's autoFocus, so the first row is focused by hand.
      (firstRow.current ?? acceptButton.current)?.focus();
    } else if (!props.sheetOpen && element.open) element.close();
  }, [props.sheetOpen, screen.plan?.id]);
  const closed = () => {
    const back = opener.current;
    opener.current = null;
    if (back?.isConnected) back.focus();
    props.onDismiss();
  };
  if (screen.unavailable) return <Text className="places-organize-note">{screen.unavailable}</Text>;
  const state = screen.running ? 'running' : screen.plan ? 'plan' : 'idle';
  const alert = props.error ? <AlertLine text={props.error} /> : null;
  return <div className="places-organize" data-state={state}>
    <div className="places-organize-bar">
      <Button variant="quiet" disabled={screen.running || props.busy} onClick={props.onOrganize}>Organize now</Button>
      <Segmented label="How to organize" value={props.mode} options={ORGANIZE_MODES} onChange={props.onMode} />
      {screen.cancel && <Button variant="ghost" disabled={props.busy} onClick={props.onCancel}>Cancel</Button>}
    </div>
    {screen.progress && <p className="places-organize-progress" role="status"><span aria-hidden="true"><StatusMark status="running" label="Organizing" dense /></span><span>{screen.progress}</span></p>}
    {screen.last && <p className="places-organize-last">{screen.last}</p>}
    {props.undo && <Button variant="ghost" disabled={props.busy} onClick={props.onUndo}>Undo</Button>}
    {screen.plan && !props.sheetOpen && <Button variant="ghost" onClick={props.onReview}>Review</Button>}
    {!props.sheetOpen && alert}
    <dialog ref={dialog} className="places-organize-sheet" aria-labelledby={titleId} onClose={closed}>
      {props.sheetOpen && screen.plan && <div className="places-organize-sheet-body">
        <SectionHeading id={titleId}>Review</SectionHeading>
        {screen.plan.summary && <p className="places-organize-summary">{screen.plan.summary}</p>}
        {screen.plan.rows.length > 0 && <ul className="places-organize-ops">
          {screen.plan.rows.map((row, position) => {
            const on = props.included.has(row.index);
            return <li key={row.index}>
              <Button ref={position === 0 ? firstRow : undefined} variant="ghost" className="places-organize-op" role="checkbox" aria-checked={on} onClick={() => props.onToggle(row.index)}>
                <span className="places-organize-check" aria-hidden="true"><Icon name="check" size="sm" /></span>
                <span>{row.line}</span>
              </Button>
            </li>;
          })}
        </ul>}
        {alert}
        <div className="places-organize-foot">
          <Button variant="ghost" disabled={props.busy} onClick={props.onDecline}>Not now</Button>
          <Button ref={acceptButton} variant="quiet" disabled={props.busy} onClick={props.onAccept}>Accept</Button>
        </div>
      </div>}
    </dialog>
  </div>;
}

/** Live block. Mounted only after GET /places/organize answers, so a missing route draws nothing. */
function PlacesOrganizeLive({ initial }: { initial: OrganizeSnapshot }) {
  const primary = useWorld(selectOrganizing);
  const secondary = useSyncExternalStore(worldStore.subscribe, selectStoredOrganizing, selectStoredOrganizing);
  const [snap, setSnap] = useState(initial);
  const [mode, setMode] = useState<OrganizeMode>('add-on');
  const [included, setIncluded] = useState<ReadonlySet<number>>(() => initialIncluded(initial.plan?.ops ?? []));
  const [sheet, setSheet] = useState(!!initial.plan);
  const [undoId, setUndoId] = useState(heldReceipt);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // A plan we already accepted or declined must not pop the sheet open again when the status is re-read.
  const finished = useRef('');
  const planId = snap.plan?.id;
  useEffect(() => {
    const plan = snap.plan;
    if (!plan || plan.id === finished.current) return;
    setIncluded(initialIncluded(plan.ops));
    setSheet(true);
    // The checks reset when the plan id changes. A refresh of the same id leaves them.
  }, [planId]); // planId is the only signal; snap.plan is the plan that arrived with it
  const feed = preferFeed(asFeed(primary), asFeed(secondary));
  const shown = organizeScreen(snap, feed);
  const run = async (job: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try { await job(); } catch (caught) {
      setError(caught instanceof Error && caught.message ? caught.message : 'That did not work.');
    } finally { setBusy(false); }
  };
  const refresh = async () => { setSnap(await organizeClient.read()); };
  return <PlacesOrganizeView
    screen={shown}
    mode={mode}
    onMode={setMode}
    included={included}
    onToggle={index => setIncluded(current => {
      const next = new Set(current);
      if (next.has(index)) next.delete(index); else next.add(index);
      return next;
    })}
    sheetOpen={sheet && !!shown.plan}
    busy={busy}
    error={error}
    undo={!!undoId}
    onOrganize={() => {
      const chosen = mode;
      void run(async () => { setSnap(await organizeClient.start(chosen)); });
    }}
    onCancel={() => void run(async () => { setSnap(await organizeClient.cancel()); })}
    onReview={() => setSheet(true)}
    onDismiss={() => setSheet(false)}
    onDecline={() => void run(async () => {
      if (!snap.plan) return;
      finished.current = snap.plan.id;
      await organizeClient.decline(snap.plan.id);
      setSheet(false);
      await refresh();
    })}
    onAccept={() => void run(async () => {
      if (!snap.plan) return;
      finished.current = snap.plan.id;
      const id = await organizeClient.accept(snap.plan.id, skipOf(snap.plan.ops, included));
      heldReceipt = id;
      setUndoId(id);
      setSheet(false);
      setSnap(current => current ? { ...current, plan: undefined, running: false } : current);
      await refresh();
    })}
    onUndo={() => void run(async () => {
      if (!undoId) return;
      await organizeClient.undo(undoId);
      heldReceipt = '';
      setUndoId('');
      await refresh();
    })}
  />;
}

/** Settings mount. Absent until the engine serves an organizing status. */
export function PlacesOrganize() {
  const [snap, setSnap] = useState<OrganizeSnapshot | null>(null);
  const [failure, setFailure] = useState('');
  useEffect(() => {
    let gone = false;
    organizeClient.read().then(next => { if (!gone) setSnap(next); }).catch((error: unknown) => {
      // 404 and an engine that is not there: the control is absent. Any other refusal is one sentence.
      if (gone) return;
      if (error instanceof OrganizeError && (error.status === 404 || error.unreachable)) return;
      setFailure(error instanceof Error && error.message ? error.message : 'Places could not be organized.');
    });
    return () => { gone = true; };
  }, []);
  if (failure) return <Text className="places-organize-note">{failure}</Text>;
  if (!snap) return null;
  return <PlacesOrganizeLive initial={snap} />;
}

/** Measurement page (`?specimen=places-organize&fixture=`). The product never opens this. */
export function PlacesOrganizeSpecimen() {
  const name = new URLSearchParams(window.location.search).get('fixture') ?? 'idle-empty';
  const fixture = organizeFixtures[name] ?? organizeFixtures['idle-empty'];
  const screen = screenOf(fixture);
  const [mode, setMode] = useState<OrganizeMode>('add-on');
  const [included, setIncluded] = useState(() => initialIncluded(fixture.snapshot?.plan?.ops ?? []));
  const [sheet, setSheet] = useState(!!screen.plan);
  const [skip, setSkip] = useState('');
  const [declined, setDeclined] = useState(false);
  const [cancelled, setCancelled] = useState(false);
  const [undone, setUndone] = useState(false);
  return <div data-fixture={name} data-skip={skip} data-declined={declined || undefined} data-cancelled={cancelled || undefined} data-undone={undone || undefined}>
    <PlacesOrganizeView
      screen={screen}
      mode={mode}
      onMode={setMode}
      included={included}
      onToggle={index => setIncluded(current => {
        const next = new Set(current);
        if (next.has(index)) next.delete(index); else next.add(index);
        return next;
      })}
      sheetOpen={sheet && !!screen.plan}
      busy={false}
      error=""
      undo={name === 'accepted'}
      onOrganize={() => undefined}
      onCancel={() => setCancelled(true)}
      onReview={() => setSheet(true)}
      onDismiss={() => setSheet(false)}
      onDecline={() => { setDeclined(true); setSheet(false); }}
      onAccept={() => setSkip(skipOf(fixture.snapshot?.plan?.ops ?? [], included).join(' '))}
      onUndo={() => setUndone(true)}
    />
  </div>;
}

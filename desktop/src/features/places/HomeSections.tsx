import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { Button, Icon, IconButton, Row, RowActions, SectionLabel, Text } from '../../components/ui';
import { LiveRows } from './live/LiveRows';
import { PlaceTile, PlaceTileGrid } from './components/PlaceTile';
import type { TintName } from './components/PlaceSwatch';
import { childMeta, nameProblem, type HomeAttention, type HomeSource, type HomeChild, type HomeConnection } from './home-model';
import { canDropOn, dropMode, placeMenu, readDrag, writeDrag, type DropPayload, type PlaceActions } from './place-actions';
import { type PlaceDeleteState } from './DeletePlaceConfirm';
import { createDecisionsClient } from '../decisions/client';
import type { DecideStatus } from '../decisions/StatusLine';
import { DECIDED_CAP, type DecidedItem } from '../decisions/decidedModel';
import { createKnowsClient, type KnowsAnswer, type KnowsMutation } from './knows/client';
import type { KnowsActions } from './knows/KnowsList';
import { createPlacesClient, placesTransport } from './client';
import { homeDecisions, homeStatus } from './home-sections-data';
import './home.css';

const decisionsClient = createDecisionsClient();
const knowsClient = createKnowsClient();
const placesClient = createPlacesClient();
type PlaceSections = { id: string; status?: DecideStatus; decisions?: DecidedItem[]; knowledge?: KnowsAnswer; error?: string };

/** Independent reads keep available sections visible when another engine door fails. Aborting prevents another place's data from arriving here. */
export function useHomeSections(placeId: string | undefined, revision: unknown, paused: boolean) {
  const [data, setData] = useState<PlaceSections>();
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!placeId || paused) return;
    const abort = new AbortController();
    const save = (patch: Partial<PlaceSections>) => {
      if (!abort.signal.aborted) setData(before => ({ ...(before?.id === placeId ? before : {}), id: placeId, ...patch }));
    };
    save({ error: undefined });
    const fail = (failure: unknown) => save({ error: failure instanceof Error ? failure.message : 'Could not read this Home.' });
    void decisionsClient.status(placeId, abort.signal).then(value => save({ status: homeStatus(value) })).catch(fail);
    // All is capped by the shared DecidedRows model, so this read requests the same ceiling.
    void placesTransport(`/places/${encodeURIComponent(placeId)}/decisions?limit=${DECIDED_CAP}`, { method: 'GET', signal: abort.signal }).then(value => save({ decisions: homeDecisions(value) })).catch(fail);
    void knowsClient.list(placeId, abort.signal).then(knowledge => save({ knowledge })).catch(fail);
    return () => abort.abort();
  }, [placeId, revision, paused, attempt]);
  const current = data?.id === placeId ? data : undefined;
  const knowledge = current?.knowledge;
  const refresh = () => setAttempt(value => value + 1);
  const changed = async (job: Promise<KnowsMutation>) => {
    const result = await job;
    if (result.ask) throw new Error(`This conflicts with “${result.ask.a.text}”. Nothing was changed.`);
    refresh();
    return { undo: async () => { await placesClient.undo(result.undo); refresh(); } };
  };
  const actions: KnowsActions = placeId && knowledge && !paused ? {
    add: text => changed(knowsClient.add(placeId, { text, ifRevision: knowledge.revision })),
    edit: (id, text) => changed(knowsClient.edit(placeId, id, { text, ifRevision: knowledge.revision })),
    remove: id => changed(knowsClient.remove(placeId, id, knowledge.revision)),
    confirm: async id => { await knowsClient.confirm(placeId, id, knowledge.revision); refresh(); },
  } : {};
  return { ...current, actions, refresh };
}

/** Runs an owner's callback and keeps its failure as a sentence. The store's errors are already readable ("That would put “A” inside “B”."),
 * so they are shown as they come; a half-finished multi-step write is the owner's to describe in its message. */
export function useRunner() {
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const run = useCallback(async (job: () => void | Promise<void>): Promise<boolean> => {
    setError(undefined);
    setBusy(true);
    try { await job(); return true; }
    catch (failure) { setError(failure instanceof Error && failure.message ? failure.message : 'That did not work. Nothing was changed.'); return false; }
    finally { setBusy(false); }
  }, []);
  return { run, error, busy, clear: () => setError(undefined) };
}
export type Runner = ReturnType<typeof useRunner>;

/** The item being dragged, kept in state because a browser hides drag data from the drop target until the drop itself. */
export type DragState = { payload: DropPayload | undefined; set: (payload: DropPayload | undefined) => void };
export function useDragState(): DragState {
  const [payload, set] = useState<DropPayload | undefined>();
  return { payload, set };
}

/** The place feed includes detached work, so closing its tab never removes a Live row. */
export function HomeAttentionSection({ items, actions, readOnly }: { items: readonly HomeAttention[]; actions: PlaceActions; readOnly?: boolean }) {
  const runner = useRunner();
  return <>
    <LiveRows items={items} readOnly={readOnly} onOpen={actions.openChat && (id => void runner.run(() => actions.openChat?.(id)))}
      onOpenInNewTab={actions.openChatInNewTab && (id => void runner.run(() => actions.openChatInNewTab?.(id)))}/>
    {runner.error && <p className="home-quiet" role="alert">{runner.error}</p>}
  </>;
}

export { PlaceChats as HomeChatsSection } from './home/PlaceChats';

/** The tile grid for a place's children, or a root's top-level places, or a search's results. It owns the inline create/rename tile and every drop. */
export function HomePlacesSection({ label, places, parentId, parentName, parentTint, actions, readOnly, siblings, runner, drag, onDelete, allowNew = true, showLabel = true, newLabel, extraTiles, restore }: {
  label: string; places: readonly HomeChild[]; parentId?: string; parentName?: string; parentTint?: TintName;
  actions: PlaceActions; readOnly?: boolean; siblings: readonly string[]; runner: Runner; drag: DragState; onDelete?: (place: HomeChild) => void; allowNew?: boolean; showLabel?: boolean; newLabel?: string; extraTiles?: ReactNode;
  /** Archived tiles offer Restore instead of Go to's neighbours: drawn as the same tiles, with their menu alone changed. */
  restore?: boolean;
}) {
  const [selected, setSelected] = useState<string>();
  const [dropOn, setDropOn] = useState<string>();
  const [creating, setCreating] = useState<{ name: string; tint: TintName } | undefined>();
  const [renaming, setRenaming] = useState<{ id: string; name: string; tint: TintName; original: string; originalTint: TintName } | undefined>();
  const showNew = allowNew && !!actions.create;
  if (!places.length && !showNew && !creating && !extraTiles) return null;

  const createProblem = creating ? nameProblem(creating.name, siblings) : undefined;
  const renameProblem = renaming ? nameProblem(renaming.name, siblings, renaming.original) : undefined;
  const submitCreate = async () => {
    if (!creating || createProblem) return;
    const ok = await runner.run(() => actions.create?.({ name: creating.name.trim(), tint: creating.tint === 'graphite' ? undefined : creating.tint, parent: parentId }));
    if (ok) setCreating(undefined);
  };
  const submitRename = async () => {
    if (!renaming || renameProblem) return;
    const { id, name, tint, original, originalTint } = renaming;
    const ok = await runner.run(async () => {
      if (name.trim() !== original) await actions.rename?.(id, name.trim());
      if (tint !== originalTint && tint !== 'graphite') await actions.setTint?.(id, tint);
    });
    if (ok) setRenaming(undefined);
  };

  return <section className="home-section home-places" aria-label={label}>
    {showLabel && <SectionLabel>{label}</SectionLabel>}
    <PlaceTileGrid label={parentName ? `Places in ${parentName}` : label}>
      {places.map(place => {
        if (renaming?.id === place.id) {
          return <PlaceTile key={place.id} mode="creating" name={renaming.name} tint={renaming.tint} invalid={!!renameProblem} hint={renameProblem ?? '↵ rename · Esc cancel'}
            onNameChange={name => setRenaming(previous => previous && { ...previous, name })} onTintChange={tint => setRenaming(previous => previous && { ...previous, tint })}
            onSubmit={() => void submitRename()} onCancel={() => setRenaming(undefined)}/>;
        }
        const accepts = canDropOn(drag.payload, place.id, actions) && !readOnly && !place.archived;
        return <PlaceTile key={place.id} id={place.id} name={place.name} tint={place.tint} tintSource={place.tintSource} meta={childMeta(place)} status={place.status}
          selected={selected === place.id} dragging={drag.payload?.kind === 'place' && drag.payload.ids.includes(place.id)} dropTarget={dropOn === place.id && accepts} disabled={!actions.goTo}
          onGoTo={() => void runner.run(() => actions.goTo?.(place.id))} onOpenInNewWindow={actions.goToInNewWindow && (() => void runner.run(() => actions.goToInNewWindow?.(place.id)))}
          onQuickLook={actions.quickLook && (() => { setSelected(place.id); actions.quickLook?.(place.id); })}
          menu={placeMenu({ id: place.id, name: place.name, tint: place.tint, pinned: place.pinned, decide: place.decide, archived: place.archived || restore }, actions, {
            readOnly, canRename: !place.path, startRename: id => setRenaming({ id, name: place.name, tint: place.tintSource === 'own' ? place.tint : 'graphite', original: place.name, originalTint: place.tintSource === 'own' ? place.tint : 'graphite' }),
            startDelete: onDelete && (() => onDelete(place)) })}
          draggable={!readOnly && !!actions.file && !place.archived}
          onDragStart={event => { const payload: DropPayload = { kind: 'place', ids: [place.id] }; writeDrag(event, payload); drag.set(payload); }} onDragEnd={() => { drag.set(undefined); setDropOn(undefined); }}
          onDragEnter={event => { if (accepts) { event.preventDefault(); setDropOn(place.id); } }}
          onDragOver={event => { if (accepts) { event.preventDefault(); event.dataTransfer.dropEffect = dropMode(event) === 'move' ? 'move' : 'copy'; } }}
          onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropOn(undefined); }}
          onDrop={event => {
            setDropOn(undefined);
            const payload = readDrag(event) ?? drag.payload;
            if (!canDropOn(payload, place.id, actions) || readOnly || place.archived) return;
            event.preventDefault();
            drag.set(undefined);
            void runner.run(() => actions.file?.(payload as DropPayload, place.id, dropMode(event)));
          }}/>;
      })}
      {creating
        ? <PlaceTile mode="creating" name={creating.name} tint={creating.tint} invalid={!!createProblem && !!creating.name.trim()} hint={creating.name.trim() && createProblem ? createProblem : '↵ create · Esc cancel'}
            onNameChange={name => setCreating(previous => previous && { ...previous, name })} onTintChange={tint => setCreating(previous => previous && { ...previous, tint })}
            onSubmit={() => void submitCreate()} onCancel={() => setCreating(undefined)}/>
        : showNew && <PlaceTile mode="new" label={newLabel} disabled={readOnly || runner.busy} onCreate={() => setCreating({ name: '', tint: parentTint ?? 'graphite' })}/>}
      {extraTiles}
    </PlaceTileGrid>
  </section>;
}

const sourceWords: Record<NonNullable<HomeSource['state']>, string> = { ok: '', missing: 'missing', unreadable: 'unreadable', unknown: '' };

/** The place's own sources, with its existing add verb available even after the empty Home disappears. */
export function HomeSourcesSection({ placeId, sources, actions, readOnly, showAdd }: { placeId: string; sources: readonly HomeSource[]; actions: PlaceActions; readOnly?: boolean; showAdd?: boolean }) {
  const add = showAdd && actions.addSources && !readOnly;
  if (!sources.length && !add) return null;
  const remove = actions.removeSource && !readOnly ? actions.removeSource : undefined;
  return <section className="home-section home-sources" aria-label="Sources">
    <SectionLabel>Sources</SectionLabel>
    {sources.length > 0 && <ul className="home-source-list" aria-label="Sources">
      {sources.map(source => <li key={source.id}><Row className="home-source" data-source-id={source.id} data-state={source.state}>
        <span className="home-source-label">{source.label}</span>
        <span className="home-source-note">{[source.kind, source.state ? sourceWords[source.state] : ''].filter(Boolean).join(' · ')}</span>
        {remove && <RowActions><IconButton size="row" icon="close" iconSize="xs" label={`Remove ${source.label}`} onClick={() => void remove(placeId, source.id)}/></RowActions>}
      </Row></li>)}
    </ul>}
    {add && <div className="home-empty-actions"><Button variant="quiet" onClick={() => actions.addSources?.(placeId)}><Icon name="attach" size="xs"/>Add files or links</Button></div>}
  </section>;
}

/** The empty place of 8b: one sentence, the two optional actions that are wired, and the context line the engine wrote. Nothing else. */
export function HomeEmptyPlace({ placeId, contextLine, actions, readOnly }: { placeId: string; contextLine?: string; actions: PlaceActions; readOnly?: boolean }) {
  const add = actions.addSources && !readOnly, write = actions.writeInstructions && !readOnly;
  return <>
    <section className="home-empty" aria-label="Empty place">
      <p className="home-empty-sentence">Nothing here yet. Start a chat below, drag chats in from anywhere, or drop in what this place should know.</p>
      {(add || write) && <div className="home-empty-actions">
        {add && <Button variant="quiet" onClick={() => actions.addSources?.(placeId)}><Icon name="attach" size="xs"/>Add files or links</Button>}
        {write && <Button variant="quiet" onClick={() => actions.writeInstructions?.(placeId)}><Icon name="pencil" size="xs"/>Write instructions</Button>}
      </div>}
    </section>
    {contextLine && <p className="home-quiet home-context">{contextLine}</p>}
  </>;
}

/** Loading, a read that failed, and the engine being out of reach. With a page already on screen the last good one stays, read-only. */
export function HomeNotice({ connection, hasView, onRetry }: { connection: HomeConnection; hasView: boolean; onRetry?: () => void }) {
  if (connection.state === 'ready') return null;
  if (connection.state === 'loading') return hasView ? null : <p className="home-notice" role="status" aria-live="polite">Loading places</p>;
  const message = connection.state === 'error' ? connection.message
    : connection.message ?? (hasView ? 'Can’t reach codeaf right now. This is the last page it loaded, so changes are paused.' : 'Can’t reach codeaf right now.');
  return <div className="home-notice" role={connection.state === 'error' ? 'alert' : 'status'} data-connection={connection.state}>
    <Text tone="default">{message}</Text>
    {onRetry && <Button variant="quiet" onClick={onRetry}>Retry</Button>}
  </div>;
}

export function HomeBanner({ message, onDismiss }: { message: string | undefined; onDismiss: () => void }) {
  if (!message) return null;
  return <div className="home-notice" role="alert" data-connection="action">
    <Text tone="default">{message}</Text>
    <Button variant="quiet" onClick={onDismiss}>Dismiss</Button>
  </div>;
}

export type DeleteState = PlaceDeleteState;

/** Delete flow: preview first, confirm second. Only offered when the owner wired both the preview and the delete. */
export function useDeleteFlow(actions: PlaceActions, runner: Runner) {
  const [state, setState] = useState<PlaceDeleteState>();
  const start = actions.loadDeletePreview && actions.remove ? (place: { id: string; name: string }) => {
    setState({ id: place.id, name: place.name, phase: 'loading' });
    actions.loadDeletePreview?.(place.id).then(
      impact => setState(current => current?.id === place.id ? { ...current, phase: 'ready', impact } : current),
      failure => setState(current => current?.id === place.id ? { ...current, phase: 'error', message: failure instanceof Error ? failure.message : undefined } : current));
  } : undefined;
  const confirm = async () => { if (state && await runner.run(() => actions.remove?.(state.id))) setState(undefined); };
  return { state, start, confirm, cancel: () => setState(undefined) };
}

/** The page frame both Homes share: the scrolling column with the design's bottom fade, and the composer slot under it. `⌘↑` or `Ctrl ↑` goes up a level (Iteration 2). */
export function HomeFrame({ label, children, composer, onUp, populated }: { label: string; children: ReactNode; composer?: ReactNode; onUp?: () => void; populated?: boolean }) {
  return <section className="home-page" data-populated-place={populated || undefined} aria-label={label}
    onKeyDown={event => { if (onUp && (event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key === 'ArrowUp') { event.preventDefault(); onUp(); } }}>
    <div className="home-scroll" data-scroll-key="home" tabIndex={-1}><div className="home-column">{children}</div></div>
    {composer && <div className="home-composer">{composer}</div>}
  </section>;
}

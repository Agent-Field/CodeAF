import { useEffect, useMemo, useState } from 'react';
import type { PlaceDetail } from '../client';
import type { ChooserMode } from './contracts';
import { GoToChooser } from './GoToChooser';
import { PlaceQuickLook } from './HomePane';
import { InstructionsDialog, NameDialog, SourcesDialog } from './PlaceDialogs';
import type { DialogRequest, PlacesShell } from './PlacesShell';
import { placeRow } from './selectors';

const quoted = (name: string | undefined) => (name ? `“${name}”` : 'the place');

/** The place's full record (instructions, sources), read when a dialog about it opens and again after each write. */
function usePlaceDetail(shell: PlacesShell, placeId: string | undefined): { detail?: PlaceDetail; failure?: string } {
  const [read, setRead] = useState<{ detail?: PlaceDetail; failure?: string }>({});
  useEffect(() => {
    if (!placeId) { setRead({}); return; }
    const abort = new AbortController();
    shell.client.home(placeId, abort.signal)
      .then(digest => { if (!abort.signal.aborted) setRead({ detail: digest.place }); })
      .catch((failure: unknown) => { if (!abort.signal.aborted) setRead({ failure: failure instanceof Error ? failure.message : 'This place could not be read.' }); });
    return () => abort.abort();
  }, [shell.client, placeId, shell.places.version]);
  return read;
}

function PlaceDialog({ shell, request }: { shell: PlacesShell; request: DialogRequest }) {
  const placeId = request.kind === 'create' ? undefined : request.placeId;
  const { detail, failure } = usePlaceDetail(shell, placeId);
  const name = placeId ? shell.index?.byId.get(placeId)?.name ?? detail?.name ?? '' : '';
  const close = shell.closeDialog;
  useEffect(() => { if (failure) { shell.warn(new Error(failure)); close(); } }, [failure]);

  if (request.kind === 'create') {
    const parent = request.parent ? shell.index?.byId.get(request.parent)?.name : undefined;
    return <NameDialog open title={parent ? `New place in “${parent}”` : 'New place'} label="Name" initial={request.name ?? ''} submitLabel="Create" onClose={close}
      onSubmit={async next => {
        const [made] = await shell.write(`Created ${quoted(next)}`, () => shell.client.createPlace({ name: next, parent: request.parent }), { subject: next });
        close();
        if (made?.place && !request.parent) await shell.goTo(made.place.id);
      }}/>;
  }
  if (request.kind === 'rename') {
    return <NameDialog open title={`Rename ${quoted(name)}`} label="Name" initial={name} submitLabel="Rename" onClose={close}
      onSubmit={async next => { await shell.write(`Renamed ${quoted(name)} to ${quoted(next)}`, () => shell.client.updatePlace(request.placeId, { name: next }), { subject: next }); close(); }}/>;
  }
  if (!detail) return null;
  if (request.kind === 'instructions') {
    return <InstructionsDialog open placeName={name} initial={detail.instructions} onClose={close}
      onSave={async text => { await shell.write(`Saved the instructions of ${quoted(name)}`, () => shell.client.updatePlace(request.placeId, { instructions: text }), { subject: name }); close(); }}/>;
  }
  return <SourcesDialog open placeName={name} native={shell.native.desktop} onClose={close}
    sources={detail.sources.map(source => ({ id: source.id, kind: source.kind, label: source.label || source.check.title || source.ref, state: source.check.state }))}
    onPickFolder={() => shell.native.pickFolder(`Add a folder to “${name}”`)}
    onPickFiles={() => shell.native.pickFiles({ multiple: true, title: `Add files to “${name}”` })}
    onAdd={async asks => {
      const what = asks.length === 1 ? asks[0].ref.split(/[\\/]/).filter(Boolean).pop() ?? asks[0].ref : `${asks.length} sources`;
      // One source per write, in order; a refusal stops there and what was added stays undoable.
      await shell.write(`Added ${what} to ${quoted(name)}`, async () => {
        const done = [];
        for (const ask of asks) done.push(await shell.client.addSource(request.placeId, { kind: ask.kind, ref: ask.ref }));
        return done;
      }, { subject: name });
    }}
    onRemove={async sourceId => { await shell.write(`Removed a source from ${quoted(name)}`, () => shell.client.removeSource(request.placeId, sourceId), { subject: name }); }}/>;
}

/** What each chooser mode does with the place the person picked. The chooser itself never writes. */
function chooseFor(shell: PlacesShell, mode: ChooserMode, name: (id: string) => string | undefined) {
  return async (id: string) => {
    if (mode.kind === 'go') { await shell.goTo(id); return; }
    if (mode.kind === 'merge') {
      await shell.write(`Merged ${quoted(mode.placeName)} into ${quoted(name(id))}`, () => shell.client.mergePlace(mode.placeId, id), { subject: name(id) });
      shell.closeChooser();
      // The window was showing the place that no longer exists: it goes where that place went.
      if (shell.place === mode.placeId) await shell.goTo(id);
      return;
    }
    if (mode.kind === 'parent') {
      await shell.write(`Added ${quoted(mode.placeName)} to ${quoted(name(id))}`, () => shell.client.setParents(mode.placeId, { add: id }), { subject: name(id) });
    } else {
      await shell.write(`Added ${quoted(mode.chatTitle)} to ${quoted(name(id))}`, () => shell.client.addChats(id, [...mode.chatIds]), { subject: name(id) });
    }
    shell.closeChooser();
  };
}

/** The shell's overlays: the Go to chooser in every mode, the place dialogs, and Quick Look from the rail. Toasts go to the window's one region (components/ui/Toast). */
export function PlacesOverlays({ shell }: { shell: PlacesShell }) {
  const graph = shell.places.graph;
  const rows = useMemo(() => (graph && shell.index ? graph.places.filter(place => !place.archived).map(place => placeRow(place, shell.index!)) : []), [graph, shell.index]);
  const name = (id: string) => shell.index?.byId.get(id)?.name;
  const mode = shell.chooser;
  const now = useMemo(() => new Date(), [mode, graph]);
  return <>
    {mode && <GoToChooser open mode={mode} places={rows} childrenOf={shell.index?.childrenOf ?? new Map()} total={rows.length} now={now}
      onClose={shell.closeChooser}
      onChoose={chooseFor(shell, mode, name)}
      onChooseInNewWindow={mode.kind === 'go' ? async id => { shell.closeChooser(); await shell.goToInNewWindow(id); } : undefined}
      onCreate={async typed => {
        const [made] = await shell.write(`Created ${quoted(typed)}`, () => shell.client.createPlace({ name: typed }), { subject: typed });
        const id = made?.place?.id;
        if (!id) throw new Error(`“${typed}” was not created.`);
        await chooseFor(shell, mode, () => typed)(id);
      }}/>}
    {shell.dialog && <PlaceDialog key={JSON.stringify(shell.dialog)} shell={shell} request={shell.dialog}/>}
    {shell.lookAt && <PlaceQuickLook id={shell.lookAt} onClose={() => shell.quickLook(undefined)} actions={{ goTo: id => shell.goTo(id), goToInNewWindow: id => shell.goToInNewWindow(id) }}/>}
  </>;
}

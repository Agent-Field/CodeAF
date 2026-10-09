import { useContext, useEffect, useMemo, useState } from 'react';
import { placeShortcuts } from '../../../design/keyboard';
import { NewTabHostContext } from '../../tabs/kinds/newtab/api';
import type { PaneRenderProps } from '../../tabs/kinds/slots';
import { PlacesError, type HomeDigest } from '../client';
import { HomePage } from '../HomePage';
import { HomeQuickLook } from '../HomeQuickLook';
import type { HomeConnection } from '../home-model';
import type { PlaceActions } from '../place-actions';
import { buildHomeActions } from './homeActions';
import { HomeComposer } from './HomeComposer';
import { usePlacesShell } from './PlacesShell';
import { homeViewFromDigest } from './selectors';
import { useStale } from './useStale';
import './home-pane.css';
import { usePlaceOffers } from '../usePlaceOffers';
import { PlaceOfferLine } from '../PlaceOfferLine';

type Read = { digest?: HomeDigest; unplaced?: HomeDigest; failure?: PlacesError | Error };

/**
 * Reads one Home and keeps it current. It reads again whenever the window's graph reading moves (a write here, a
 * change on the world stream, focus coming back), so what the page shows is never older than the rail beside it.
 * A failed read keeps the last good page and says why; it never draws sample data.
 */
function useHomeRead(id: string): Read & { loading: boolean; retry: () => void } {
  const shell = usePlacesShell();
  const version = shell?.places.version ?? 0;
  const [read, setRead] = useState<Read>({});
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!shell) return;
    const abort = new AbortController();
    setLoading(true);
    Promise.all([shell.client.home(id, abort.signal), id === 'root' ? shell.client.home('now', abort.signal) : Promise.resolve(undefined)])
      .then(([digest, unplaced]) => { if (!abort.signal.aborted) setRead({ digest, unplaced }); })
      .catch((failure: unknown) => { if (!abort.signal.aborted) setRead(before => ({ ...before, failure: failure instanceof Error ? failure : new Error('This Home could not be read.') })); })
      .finally(() => { if (!abort.signal.aborted) setLoading(false); });
    return () => abort.abort();
  }, [shell?.client, id, version, attempt]);
  return { ...read, loading, retry: () => { void shell?.refresh(); setAttempt(n => n + 1); } };
}

function connectionOf(read: Read & { loading: boolean }): HomeConnection {
  if (read.failure) {
    if (read.failure instanceof PlacesError && read.failure.unreachable) return { state: 'offline', message: 'Can’t reach the engine.' };
    return { state: 'error', message: read.failure.message };
  }
  return read.digest ? { state: 'ready' } : { state: 'loading' };
}

/** Quick Look reads the looked-at place's own Home, once, and never switches the window (Places 8d). */
export function PlaceQuickLook({ id, onClose, actions }: { id: string; onClose: () => void; actions: PlaceActions }) {
  const shell = usePlacesShell();
  const [digest, setDigest] = useState<HomeDigest>();
  useEffect(() => {
    const abort = new AbortController();
    shell?.client.home(id, abort.signal).then(setDigest).catch((failure: unknown) => { if (!abort.signal.aborted) { shell.warn(failure); onClose(); } });
    return () => abort.abort();
  }, [id]);
  if (!digest) return null;
  return <HomeQuickLook view={homeViewFromDigest(digest)} actions={actions} now={new Date()} onClose={onClose}/>;
}

/**
 * The `home` tab kind: a place's Home (Places 8a, 8b), or All places (8c, 8e) when the tab's place is `root`. It
 * draws the page the Places Home feature built, fed by the engine's digest and wired to the shell. The composer
 * starts a chat in this place; All places has none, because a chat started there would belong to no place.
 */
export function HomePane({ pane, focused, actions: paneActions }: PaneRenderProps) {
  const shell = usePlacesShell();
  const strip = useContext(NewTabHostContext);
  const id = pane.place ?? 'root';
  const read = useHomeRead(id);
  const offers = usePlaceOffers(id === 'root', id, false, focused);
  const proposal = offers.offers.find(offer => (offer.kind === 'move' || offer.kind === 'create') && (offer.chatIds?.length ?? 0) > 0);
  const offerName = proposal?.kind === 'create' ? proposal.name : proposal?.placeId ? shell?.index?.byId.get(proposal.placeId)?.name : undefined;
  const [looking, setLooking] = useState<string>();
  const now = useMemo(() => new Date(), [read.digest, read.unplaced]);
  const stale = useStale(id === 'root' ? shell : undefined);
  const view = useMemo(() => {
    if (!read.digest) return undefined;
    const home = homeViewFromDigest(read.digest, shell?.places.graph, read.unplaced);
    return stale.places[0] ? { ...home, stale: stale.places[0] } : home;
  }, [read.digest, read.unplaced, shell?.places.graph, stale.places]);
  const homeActions = useMemo(() => shell ? buildHomeActions({
    shell, homeId: id, digests: [read.digest, read.unplaced], strip: strip ? { state: strip.state, dispatch: strip.dispatch } : undefined,
    quickLook: setLooking, retry: read.retry, snoozeStale: id === 'root' ? stale.snooze : undefined,
  }) : {}, [shell, id, read.digest, read.unplaced, strip?.state, strip?.dispatch, stale.snooze]);
  const connection = connectionOf(read);

  if (!shell) return <div className="home-pane"><p className="home-quiet">Places are not available in this window.</p></div>;
  const composer = view?.kind === 'place' && strip
    ? <HomeComposer shell={shell} placeId={view.id} placeName={view.title} draft={pane.draft} onDraft={paneActions.onDraft} dispatch={strip.dispatch} offline={connection.state === 'offline'}/>
    : undefined;
  return <div className="home-pane">
    <HomePage view={view} connection={connection} actions={homeActions} composer={composer} now={now} newWindowHint={placeShortcuts.openInNewWindow} suggestion={proposal && offerName && connection.state === 'ready' ? <PlaceOfferLine key={proposal.id} proposal={proposal} text={`${proposal.chatIds?.length} of these look like they belong in ${offerName}`} action={proposal.kind === 'create' ? `Create ${offerName}` : 'Move them'} onSettled={offers.refresh}/> : undefined}/>
    {looking && <PlaceQuickLook id={looking} actions={homeActions} onClose={() => setLooking(undefined)}/>}
  </div>;
}

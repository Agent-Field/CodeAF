import { useContext, useEffect, useMemo, useRef, useState } from 'react';
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
import { FirstPlaceTabOffer } from '../FirstPlaceTabOffer';
import { proposalsClient } from '../proposals-client';
import { Suggestion } from '../home/Suggestion';
import { browserSnoozes, suppliedSuggestions, writeSnooze } from '../suggestions';

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
  const [looking, setLooking] = useState<string>();
  const heldOffer = useRef(false);
  const [hiddenOffer, setHiddenOffer] = useState<string>();
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
    ? (empty: boolean) => <HomeComposer shell={shell} placeId={view.id} placeName={view.title} draft={pane.draft} onDraft={paneActions.onDraft} dispatch={strip.dispatch} empty={empty} offline={connection.state === 'offline'}/>
    : undefined;
  // A create of at least five chats is the card. A move or a file stays the sparkles line, so Move them keeps its sentence.
  const snoozes = browserSnoozes(now);
  const offered = suppliedSuggestions(offers.offers.map(offer => ({
    id: offer.id, kind: offer.kind, name: offer.name, reason: offer.reason, chatCount: offer.chatIds?.length,
    placeName: offer.kind === 'create' ? offer.name : offer.placeId ? shell.index?.byId.get(offer.placeId)?.name : undefined,
  })), now, snoozes).find(item => item.layout === 'card');
  const card = offered?.layout === 'card' ? offered : undefined;
  const hidden = new Set(snoozes.map(snooze => snooze.id));
  const lineProposal = offers.offers.find(offer => !hidden.has(offer.id) && offer.id !== hiddenOffer && (offer.kind === 'move' || offer.kind === 'file') && (offer.chatIds?.length ?? 0) > 0);
  const lineName = lineProposal?.placeId ? shell.index?.byId.get(lineProposal.placeId)?.name : undefined;
  async function settleCard(accept: boolean) {
    const proposal = card && offers.offers.find(offer => offer.id === card.id);
    if (!shell || !card || !proposal || heldOffer.current) return;
    heldOffer.current = true;
    try {
      if (accept) await shell.write(`Created “${card.title}”`, () => proposalsClient.accept(proposal), { subject: card.title });
      else {
        await proposalsClient.decline(proposal.id);
        writeSnooze(localStorage, proposal.id, new Date());
      }
      setHiddenOffer(proposal.id);
    } catch (failure) { shell.warn(failure); }
    finally { heldOffer.current = false; offers.refresh(); }
  }
  const suggestion = connection.state === 'ready' && ((card && card.id !== hiddenOffer) || (lineProposal && lineName))
    ? <>
        {card && card.id !== hiddenOffer && <Suggestion key={card.id} layout="card" kicker={card.kicker} title={card.title} detail={card.detail} label={card.label}
          actions={card.actions.map(action => ({ ...action, tone: action.id === 'not-now' ? 'quiet' : 'field', onSelect: () => settleCard(action.id === 'create') }))}/>}
        {lineProposal && lineName && <PlaceOfferLine key={lineProposal.id} proposal={lineProposal} text={`${lineProposal.chatIds?.length} of these look like they belong in ${lineName}`} action="Move them" onSettled={offers.refresh}/>}
      </>
    : read.digest?.kind === 'place' ? <FirstPlaceTabOffer key={id} digest={read.digest} active={focused}/> : undefined;
  return <div className="home-pane">
    <HomePage view={view} connection={connection} actions={homeActions} composer={composer} now={now} newWindowHint={placeShortcuts.openInNewWindow} suggestion={suggestion}/>
    {looking && <PlaceQuickLook id={looking} actions={homeActions} onClose={() => setLooking(undefined)}/>}
  </div>;
}

// The group suggestion pill (Shell 2b): a 34px pill at the top of the content card, "Group the 3 tabs as Title?" with the
// accent Group button and a quiet X. It only asks; Group dispatches the reducer's own `group` action.
import { Button, IconButton, Icon } from '../../components/ui';
import { canonicalGroupOffers } from './canonicalOffers.ts';
import type { TabsApi } from './context';
import type { GroupOffer as Offer } from './offerRules.ts';
import { useGroupOffer } from './useGroupOffer.ts';
import './group-offer.css';

/** The pill itself: presentational, so a specimen can draw it with no workspace behind it. */
export function GroupOfferPill({ offer, onGroup, onDismiss }: { offer: Offer; onGroup: () => void; onDismiss: () => void }) {
  const count = offer.ids.length;
  return <div className="group-offer" role="group" aria-label="Group suggestion">
    <Icon name="layers" size="xs"/>
    <span className="group-offer-text">{offer.title ? <>Group the {count} tabs as <b>{offer.title}</b>?</> : <>Group these {count} tabs?</>}</span>
    <Button variant="primary" className="group-offer-accept" onClick={onGroup}>Group</Button>
    <IconButton size="row" icon="close" iconSize="micro" label="Dismiss group suggestion" onClick={onDismiss}/>
  </div>;
}

/**
 * Wiring seam: render once inside `.tab-workspace` (after the PaneGrid) with the workspace's `TabsApi`. It reads
 * `api.state.tabs` and dispatches `{ type: 'group', id, ids, title }`; it keeps no tab state of its own.
 */
export function GroupOffer({ api }: { api: Pick<TabsApi, 'state' | 'dispatch' | 'overlayOpen'> & Partial<Pick<TabsApi, 'summaries'>> }) {
  const { offer, accept, dismiss } = useGroupOffer({
    tabs: api.state.tabs,
    summaries: api.summaries,
    suspended: api.overlayOpen,
    ask: canonicalGroupOffers,
    onGroup: (ids, title) => api.dispatch({ type: 'group', id: ids[0], ids: ids.slice(1), title }),
  });
  return offer ? <GroupOfferPill offer={offer} onGroup={accept} onDismiss={dismiss}/> : null;
}

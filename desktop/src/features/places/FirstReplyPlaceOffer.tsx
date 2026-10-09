import { chatIdFromSessionFile } from './client';
import { PlaceOfferLine } from './PlaceOfferLine';
import { usePlacesShell } from './shell/PlacesShell';
import { usePlaceOffers } from './usePlaceOffers';

/** The ledger, not the renderer, decides whether the first reply warrants another existing place. */
export function FirstReplyPlaceOffer({ sessionFile, focused }: { sessionFile: string; focused: boolean }) {
  const shell = usePlacesShell();
  const chatId = chatIdFromSessionFile(sessionFile);
  const read = usePlaceOffers(Boolean(chatId), chatId, true, focused);
  const proposal = read.offers.find(offer => offer.kind === 'file' && offer.chatIds?.length === 1 && offer.chatIds[0] === chatId
    && Boolean(offer.placeId && shell?.index?.byId.has(offer.placeId)));
  const name = proposal?.placeId ? shell?.index?.byId.get(proposal.placeId)?.name : undefined;
  return proposal && name ? <PlaceOfferLine key={proposal.id} proposal={proposal} text={proposal.reason} action={`Add to ${name}`} onSettled={read.refresh}/> : null;
}

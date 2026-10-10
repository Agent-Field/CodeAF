import { useMemo, useSyncExternalStore } from 'react';
import type { PlaceRowModel } from '../shell/contracts';
import { GoToPalette } from './GoToPalette';
import { currentPick, settlePick, subscribePick } from './pickPlace';

export type PlacePickerHostProps = {
  /** Every non-archived place; the host drops the excluded ones before the palette sees them. */
  places: readonly PlaceRowModel[];
  childrenOf: ReadonlyMap<string, readonly string[]>;
  now: Date;
};

/**
 * Mounts the palette in pick mode while a `pickPlace` question is pending, and answers it. Excluded places leave both
 * Recent and the tree, so a place the thing is already in can never be chosen twice.
 */
export function PlacePickerHost({ places, childrenOf, now }: PlacePickerHostProps) {
  const request = useSyncExternalStore(subscribePick, currentPick);
  const offered = useMemo(() => (request ? places.filter(place => !request.exclude.has(place.id)) : places), [places, request]);
  if (!request) return null;
  const answer = (placeId: string | undefined) => settlePick(request, placeId);
  return <GoToPalette key={request.title} open pickTitle={request.title} places={offered} childrenOf={childrenOf} now={now}
    onOpen={answer} onOpenInNewWindow={answer} onClose={() => answer(undefined)}
    // Without a creator the Create row would do nothing, so choosing it is a dismissal rather than a silent no-op.
    onCreate={name => { if (!request.create || !name) return; void request.create(name).then(answer, () => answer(undefined)); }}/>;
}

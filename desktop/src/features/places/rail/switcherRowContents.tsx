import { PlaceSwatch } from '../components/PlaceSwatch';
import { PlaceDot } from '../PlaceDot';
import type { PlaceRowModel } from '../shell/contracts';
import './rail-places.css';

/** The collapsed menu keeps the same two marks as the rail, without coupling the shared menu to places. */
export function switcherRowContents(place: PlaceRowModel) {
  return {
    label: place.name,
    lead: <PlaceSwatch tint={place.tint} role="rail"/>,
    labelSuffix: place.parentName ? <span className="rail-place-path"> · {place.parentName}</span> : undefined,
    trail: <PlaceDot status={place.status} label={place.statusLabel}/>,
  };
}

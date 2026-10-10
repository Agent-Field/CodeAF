import type { MouseEvent } from 'react';
import { NavigationItem } from '../../components/ui';
import { PlaceDot } from '../places/PlaceDot';
import { nowCount } from './nowCount';
import type { PlaceRailProps } from './PlaceRail';

type NowModel = PlaceRailProps['now'] & { hover?: boolean };

/**
 * The Now row (Shell 3a, C-PLACE-5): circle-dashed, "Now", the number of things running in ink-3 and, when something
 * needs the person, the one amber dot. There is no Inbox row beside it: Iteration 2 I2.1 moved that to the frame pill.
 * Now draws no menu (Interactions, right-click "—"); swallowing the event keeps the webview's own menu off the row.
 */
export function NowRow({ now, primaryClick }: { now: NowModel; primaryClick: (event: MouseEvent) => boolean }) {
  const count = nowCount(now.count);
  return <NavigationItem icon="now" active={now.active} data-hover={now.hover || undefined} aria-keyshortcuts={now.shortcut}
    onContextMenu={event => event.preventDefault()} onClick={event => (primaryClick(event) && now.onNewWindow ? now.onNewWindow() : now.onGo())}
    onAuxClick={event => { if (event.button === 1 && now.onNewWindow) { event.preventDefault(); now.onNewWindow(); } }}
    trail={<>{count !== undefined && <span className="rail-meta" data-rail-count>{count}</span>}<PlaceDot status={now.status} label={now.statusLabel}/></>}>Now</NavigationItem>;
}

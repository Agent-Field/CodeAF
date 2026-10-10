import type { MouseEvent } from 'react';
import { RailRow, type RailRowProps } from '../places/rail/RailRow';
import { nowCount } from './nowCount';
import type { RailNow } from '../places/rail/PlaceRail';

type NowModel = RailNow;

/**
 * The Now row (Shell 3a, Places 8e): circle-dashed, "Now", and the unplaced-chat count in ink-3.
 * Zero draws nothing. The count is described to a screen reader and kept out of the row's name,
 * so the row stays "Now". An amber dot is the only other mark, when something needs the person.
 * There is no Inbox row beside it: Iteration 2 I2.1 moved that to the frame pill.
 * Now draws no menu (Interactions, right-click "—"); swallowing the event keeps the webview's own menu off the row.
 */
export function NowRow({ now, primaryClick, ...row }: { now: NowModel; primaryClick: (event: MouseEvent) => boolean } & Omit<RailRowProps, 'name' | 'icon' | 'active' | 'meta' | 'status' | 'statusLabel'>) {
  const count = nowCount(now.count);
  return <RailRow {...row} name="Now" icon="now" active={now.active} data-hover={now.hover || undefined} aria-keyshortcuts={now.shortcut}
    aria-description={count !== undefined ? `${count} unplaced` : undefined}
    onContextMenu={event => event.preventDefault()} onClick={event => (primaryClick(event) && now.onNewWindow ? now.onNewWindow() : now.onGo())}
    onAuxClick={event => { if (event.button === 1 && now.onNewWindow) { event.preventDefault(); now.onNewWindow(); } }}
    meta={count !== undefined ? <span data-rail-count aria-hidden="true">{count}</span> : undefined} status={now.status} statusLabel={now.statusLabel}/>;
}

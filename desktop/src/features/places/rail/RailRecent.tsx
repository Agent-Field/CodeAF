import { useMemo, useState } from 'react';
import { Button, ContextMenu, Icon, type MenuEntry } from '../../../components/ui';
import { StatusMark } from '../../../components/ui/StatusMark';
import { shellShortcuts } from '../../../design/keyboard';
import { windowPlace } from '../../../lib/native/windowPlace';
import { recentAge, readRecentCollapsed, useRecent, writeRecentCollapsed, type RecentItem } from '../../shell/recent';
import { requestOpenKind } from '../../shell/shellState';
import { PlaceSwatch, type TintName } from '../components/PlaceSwatch';
import { usePlacesShell } from '../shell/PlacesShell';
import { requestWorkspace } from '../shell/workspaceBus';
import { RailRow } from './RailRow';

const background = (event: { metaKey: boolean; ctrlKey: boolean }) => event.metaKey || event.ctrlKey;

/** The one place the rail asks the workspace about a recent tab. Every verb is an action on the list ⌘⇧T reopens from. */
const open = (id: string, inBackground = false) => requestWorkspace({ type: 'reopen-id', id, nextToActive: true, background: inBackground });
const split = (id: string) => requestWorkspace({ type: 'reopen-split', id });
const forget = (id?: string) => requestWorkspace({ type: 'forget-closed', id });

/** The right-click menu of one row. Forget takes it off Recent only; the conversation itself stays in History. */
function menuFor(item: RecentItem): MenuEntry[] {
  return [
    { id: 'open', label: 'Open', onSelect: () => open(item.id) },
    { id: 'split', label: 'Open in split', onSelect: () => split(item.id) },
    { id: 'forget', label: 'Forget', onSelect: () => forget(item.id) },
    { kind: 'separator', id: 'rule' },
    { id: 'clear', label: 'Clear recent', onSelect: () => forget() },
  ];
}

/** Colour lives only on the 6px mark: running work is the accent, a question is amber, a failure is red. */
function meta(item: RecentItem, tint: TintName | undefined, age: string | undefined) {
  const parts = [tint && <PlaceSwatch key="place" tint={tint} role="rail"/>, item.mark === 'working' && <StatusMark key="mark" status="running" label="Running" dense/>, age && <span key="age">{age}</span>];
  return parts.some(Boolean) ? <span className="rail-recent-meta">{parts}</span> : undefined;
}

/**
 * The last things you closed or left, one click from the rail (owner decision 2026-10-11). Same rows, type and hover
 * as the places above it. Nothing recent means no section at all; a collapsed rail draws none of it.
 */
export function RailRecent({ focusProps }: { focusProps: (id: string) => { 'data-rail-item': string; tabIndex: number; onFocus: () => void } }) {
  const items = useRecent();
  const shell = usePlacesShell();
  const label = useMemo(() => windowPlace().label, []);
  const [collapsed, setCollapsed] = useState(() => readRecentCollapsed(label));
  if (!items.length) return null;
  const toggle = () => { writeRecentCollapsed(label, !collapsed); setCollapsed(!collapsed); };
  const now = Date.now();
  return <section className="rail-group rail-recent" aria-label="Recent">
    <div className="rail-section-label">
      <Button variant="ghost" className="rail-recent-head" aria-expanded={!collapsed} onClick={toggle}>
        <span>Recent</span><Icon name={collapsed ? 'chevronRight' : 'chevron'} size="xs"/>
      </Button>
    </div>
    {!collapsed && <>
      <div className="rail-rows">
        {items.map(item => {
          const tint = item.placeId ? shell?.index?.byId.get(item.placeId)?.effectiveTint as TintName | undefined : undefined;
          return <ContextMenu key={item.id} label={`${item.title} actions`} items={menuFor(item)}>
            <RailRow name={item.title} icon={item.icon} active={false} status={item.mark === 'waiting' || item.mark === 'failed' ? item.mark : undefined}
              statusLabel={item.mark === 'waiting' ? 'Needs you' : undefined} meta={meta(item, tint, recentAge(item.closedAt, now))}
              close={{ onClose: () => forget(item.id), label: `Forget ${item.title}` }}
              {...focusProps(`recent:${item.id}`)} onClick={event => open(item.id, background(event))}/>
          </ContextMenu>;
        })}
      </div>
      <RailRow name="History" icon="history" active={false} meta={<span aria-hidden="true">{shellShortcuts.history}</span>}
        aria-keyshortcuts="Meta+Y Control+Y" {...focusProps('recent:history')} onClick={() => requestOpenKind('history')}/>
    </>}
  </section>;
}

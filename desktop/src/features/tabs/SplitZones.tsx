import { useRef, useState, type Dispatch, type ReactNode } from 'react';
import { Icon } from '../../components/ui';
import { useDropTarget } from '../../components/ui/usePointerDrag';
import { edgeLabel, edgeMerge, edgeZones, type EdgeZone } from './hosts/dragHost';
import type { WorkspaceAction } from './model';
import './split-zones.css';

/**
 * Design 2g: while a tab is dragged over the content, the left, right and bottom edges show a faint
 * outline; hovering one fills the half it would take with a translucent accent zone and a "Split …" pill,
 * and dropping merges the tab into this one as a new pane (up to a 2x2 grid).
 * The bands and the preview are both targets: the preview covers the band once it appears, so the
 * pointer would miss the band if only the band were registered.
 */
export function SplitZones({ hostId, guestId, dispatch }: { hostId: string; guestId: string; dispatch: Dispatch<WorkspaceAction> }) {
  const [over, setOver] = useState<EdgeZone | null>(null);
  const live = useRef<EdgeZone | null>(null);
  const setZone = (zone: EdgeZone | null) => { live.current = zone; setOver(zone); };
  const drop = (zone: EdgeZone) => {
    setZone(null);
    dispatch({ type: 'split-merge', id: hostId, withId: guestId, ...edgeMerge(zone) });
  };
  return (
    <div className="split-zones" aria-hidden="true">
      {edgeZones.map(zone => <ZoneTarget key={zone} zone={zone} live={live} setZone={setZone} onDrop={drop} className="split-zone-band" over={over}/>)}
      {over && <ZoneTarget zone={over} live={live} setZone={setZone} onDrop={drop} className="split-zone-preview" over={over}>
        <span className="split-zone-pill"><Icon name="split" size="xs"/>{edgeLabel[over]}</span>
      </ZoneTarget>}
    </div>
  );
}

function ZoneTarget({ zone, live, setZone, onDrop, className, over, children }: {
  zone: EdgeZone; live: { current: EdgeZone | null }; setZone: (zone: EdgeZone | null) => void; onDrop: (zone: EdgeZone) => void;
  className: string; over: EdgeZone | null; children?: ReactNode;
}) {
  const ref = useDropTarget({
    kind: 'edge-zone',
    id: `${className}:${zone}`,
    accepts: (payload) => payload.kind === 'tab',
    hover: () => setZone(zone),
    // A move from the band onto its own preview leaves one target and enters the other in the same event.
    // The ref is already the new zone by the time a stale leave would clear it, so only a real exit wins.
    leave: () => { if (live.current === zone) setZone(null); },
    drop: () => { onDrop(zone); return true; },
  });
  return <div ref={ref} className={className} data-zone={zone} data-over={over === zone || undefined}>{children}</div>;
}

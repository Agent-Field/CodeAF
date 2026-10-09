import { useState, type DragEvent, type Dispatch } from 'react';
import { Icon } from '../../components/ui';
import { edgeLabel, edgeMerge, edgeZones, endTabDrag, tabDragType, type EdgeZone } from './hosts/dragHost';
import type { WorkspaceAction } from './model';
import './split-zones.css';

/**
 * Design 2g: while a tab is dragged over the content, the left, right and bottom edges show a faint
 * outline; hovering one fills the half it would take with a translucent accent zone and a "Split …" pill,
 * and dropping merges the tab into this one as a new pane (up to a 2x2 grid).
 */
export function SplitZones({ hostId, guestId, dispatch }: { hostId: string; guestId: string; dispatch: Dispatch<WorkspaceAction> }) {
  const [over, setOver] = useState<EdgeZone | null>(null);
  const hover = (zone: EdgeZone) => (event: DragEvent) => { event.preventDefault(); event.stopPropagation(); event.dataTransfer.dropEffect = 'move'; setOver(zone); };
  const leave = (zone: EdgeZone) => (event: DragEvent) => {
    const next = event.relatedTarget instanceof Element ? event.relatedTarget.closest<HTMLElement>('.split-zone-band, .split-zone-preview') : null;
    // Entering the highlighted half (or its pill) keeps the same drop intent.
    if (next?.dataset.zone === zone) return;
    setOver(current => current === zone ? null : current);
  };
  const drop = (zone: EdgeZone) => (event: DragEvent) => {
    event.preventDefault();
    event.stopPropagation();
    const withId = event.dataTransfer.getData(tabDragType) || guestId;
    setOver(null);
    endTabDrag();
    dispatch({ type: 'split-merge', id: hostId, withId, ...edgeMerge(zone) });
  };
  return (
    <div className="split-zones" aria-hidden="true">
      {edgeZones.map(zone => <div key={zone} className="split-zone-band" data-zone={zone} data-over={over === zone || undefined} onDragEnter={hover(zone)} onDragOver={hover(zone)} onDragLeave={leave(zone)} onDrop={drop(zone)}/>)}
      {over && <div className="split-zone-preview" data-zone={over} onDragEnter={hover(over)} onDragOver={hover(over)} onDragLeave={leave(over)} onDrop={drop(over)}><span className="split-zone-pill"><Icon name="split" size="xs"/>{edgeLabel[over]}</span></div>}
    </div>
  );
}

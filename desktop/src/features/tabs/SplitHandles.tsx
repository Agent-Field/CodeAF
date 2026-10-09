import { useCallback, useEffect, useLayoutEffect, useRef, type Dispatch, type KeyboardEvent, type PointerEvent, type RefObject } from 'react';
import design from '../../design/tokens.json';
import { clampRatio } from './helpers';
import type { Tab, WorkspaceAction } from './model';

const step = 0.02;
const even = { col: 0.5, row: 0.5 };
const px = (name: keyof typeof design.foundation) => parseFloat(design.foundation[name]);

type Axis = 'col' | 'row';

/**
 * The dividers of a split (design 2h: "resize handles show on hover"). A handle is an 8px hit area in the gap
 * between two cards. Dragging moves the divider live on the grid and commits the ratio to the model on release;
 * the arrow keys move it in 2% steps. Geometry is applied straight to the grid, so a drag never re-renders a pane.
 */
export function SplitHandles({ tab, grid, dispatch }: { tab: Tab; grid: RefObject<HTMLDivElement | null>; dispatch: Dispatch<WorkspaceAction> }) {
  const split = tab.split!;
  const count = split.panes.length;
  const hasCol = split.layout !== '2x1';
  const hasRow = split.layout !== '1x2';
  const handles = { col: useRef<HTMLDivElement>(null), row: useRef<HTMLDivElement>(null) };
  const live = useRef({ ...even, ...split.ratios });
  const dragging = useRef<Axis | null>(null);

  const place = useCallback(() => {
    const el = grid.current;
    if (!el) return;
    const { col, row } = live.current;
    el.style.gridTemplateColumns = hasCol ? `minmax(0, ${col}fr) minmax(0, ${1 - col}fr)` : '';
    el.style.gridTemplateRows = hasRow ? `minmax(0, ${row}fr) minmax(0, ${1 - row}fr)` : '';
    const box = el.getBoundingClientRect();
    const panes = el.querySelectorAll<HTMLElement>(':scope > .workspace-pane');
    const hit = px('pane-resize-hit');
    const gap = px('pane-gap');
    const first = panes[0]?.getBoundingClientRect();
    if (!first) return;
    const colHandle = handles.col.current;
    if (colHandle) {
      colHandle.style.left = `${first.right + gap / 2 - hit / 2 - box.left}px`;
      colHandle.style.top = `${first.top - box.top}px`;
      // Three panes in a 2x2 share the bottom row, so the vertical divider stops at the top row.
      colHandle.style.height = `${hasRow && count === 3 ? first.height : box.height}px`;
      colHandle.style.width = `${hit}px`;
    }
    const rowHandle = handles.row.current;
    const lower = panes[hasCol ? 2 : 1]?.getBoundingClientRect();
    if (rowHandle && lower) {
      rowHandle.style.top = `${lower.top - gap / 2 - hit / 2 - box.top}px`;
      rowHandle.style.left = '0px';
      rowHandle.style.width = `${box.width}px`;
      rowHandle.style.height = `${hit}px`;
    }
  }, [hasCol, hasRow, count]);

  useLayoutEffect(() => {
    live.current = { ...even, ...split.ratios };
    place();
    const el = grid.current;
    // The grid element outlives the split, so leave it as the stylesheet draws it.
    return () => { if (el) { el.style.gridTemplateColumns = ''; el.style.gridTemplateRows = ''; } };
  }, [tab.id, split.ratios?.col, split.ratios?.row, split.layout, count, place]);
  useEffect(() => {
    const el = grid.current;
    if (!el) return;
    const observer = new ResizeObserver(place);
    observer.observe(el);
    return () => observer.disconnect();
  }, [place]);

  const commit = (axis: Axis, value: number) => dispatch({ type: 'split-resize', id: tab.id, [axis]: value });
  const share = (axis: Axis, event: PointerEvent) => {
    const box = grid.current!.getBoundingClientRect();
    const gap = px('pane-gap');
    return axis === 'col' ? (event.clientX - box.left - gap / 2) / (box.width - gap) : (event.clientY - box.top - gap / 2) / (box.height - gap);
  };
  const start = (axis: Axis) => (event: PointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragging.current = axis;
    event.currentTarget.dataset.dragging = '';
  };
  const move = (axis: Axis) => (event: PointerEvent<HTMLDivElement>) => {
    if (dragging.current !== axis) return;
    live.current = { ...live.current, [axis]: clampRatio(share(axis, event)) };
    place();
  };
  const end = (axis: Axis) => (event: PointerEvent<HTMLDivElement>) => {
    if (dragging.current !== axis) return;
    dragging.current = null;
    delete event.currentTarget.dataset.dragging;
    commit(axis, live.current[axis]);
  };
  const key = (axis: Axis) => (event: KeyboardEvent) => {
    const back = axis === 'col' ? 'ArrowLeft' : 'ArrowUp';
    const forward = axis === 'col' ? 'ArrowRight' : 'ArrowDown';
    if (event.key !== back && event.key !== forward) return;
    event.preventDefault();
    commit(axis, live.current[axis] + (event.key === forward ? step : -step));
  };
  const handle = (axis: Axis) => (
    <div ref={handles[axis]} className="pane-resize" data-axis={axis} role="separator" aria-orientation={axis === 'col' ? 'vertical' : 'horizontal'}
      aria-label={axis === 'col' ? 'Resize panes side by side' : 'Resize panes top and bottom'} aria-valuemin={Math.round(clampRatio(0) * 100)} aria-valuemax={Math.round(clampRatio(1) * 100)} aria-valuenow={Math.round(live.current[axis] * 100)} tabIndex={0}
      onPointerDown={start(axis)} onPointerMove={move(axis)} onPointerUp={end(axis)} onPointerCancel={end(axis)} onKeyDown={key(axis)} onDoubleClick={() => { live.current = { ...live.current, [axis]: even[axis] }; place(); commit(axis, even[axis]); }}/>
  );
  return <>{hasCol && handle('col')}{hasRow && handle('row')}</>;
}

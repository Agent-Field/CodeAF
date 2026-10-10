// React door onto the one pointer-drag session. The source handler and the drop
// registration read the latest callbacks, so a row that re-renders mid-gesture
// still drops into the list it belongs to now.
import { useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type RefCallback } from 'react';
import { armPointerDrag, registerDropTarget, type DragSource, type DropRegistration } from './pointerDrag';

export type { DragPayload, DragPoint, DropKind, DropRegistration } from './pointerDrag';
import './pointer-drag.css';

export function usePointerDrag(source: DragSource | null): { onPointerDown: (event: ReactPointerEvent<HTMLElement>) => void } {
  const sourceRef = useRef(source);
  sourceRef.current = source;
  return {
    onPointerDown: (event) => {
      const current = sourceRef.current;
      const node = event.currentTarget;
      if (!current || current.enabled === false || !(node instanceof HTMLElement)) return;
      armPointerDrag(event.nativeEvent, current, node);
    },
  };
}

/**
 * Registers the node as a drop target for as long as it is mounted and `handlers`
 * is set. Passing null removes it, which is how a pinned overview section stays
 * out of the gesture.
 */
export function useDropTarget(handlers: DropRegistration | null): RefCallback<HTMLElement> {
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;
  const [node, setNode] = useState<HTMLElement | null>(null);
  useLayoutEffect(() => {
    const current = handlersRef.current;
    if (!node || !current) return;
    return registerDropTarget(node, {
      kind: current.kind,
      id: current.id,
      accepts: (payload) => handlersRef.current?.accepts(payload) ?? false,
      hover: (point, payload) => handlersRef.current?.hover(point, payload),
      leave: () => handlersRef.current?.leave(),
      drop: (point, payload) => handlersRef.current?.drop(point, payload) ?? false,
    });
  }, [node, handlers?.kind, handlers?.id, handlers === null]);
  return setNode;
}

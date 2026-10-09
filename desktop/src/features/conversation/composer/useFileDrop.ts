import { useEffect, useState, type DragEvent } from 'react';

const carriesFiles = (event: { dataTransfer: DataTransfer | null }) =>
  Array.from(event.dataTransfer?.types ?? []).includes('Files');

type NativeDrag = globalThis.DragEvent;

/**
 * pageDrag: files are being dragged anywhere over the window (shows the quiet overlay).
 * over: they are over the composer itself. A stray drop elsewhere must not navigate away.
 */
export function useFileDrop(onFiles: (files: File[]) => void, enabled: boolean) {
  const [pageDrag, setPageDrag] = useState(false);
  const [over, setOver] = useState(false);

  useEffect(() => {
    if (!enabled) return;
    let depth = 0;
    const enter = (event: NativeDrag) => {
      if (!carriesFiles(event)) return;
      depth += 1;
      setPageDrag(true);
    };
    const leave = (event: NativeDrag) => {
      if (!carriesFiles(event)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) setPageDrag(false);
    };
    const hover = (event: NativeDrag) => {
      if (carriesFiles(event)) event.preventDefault();
    };
    const end = (event: NativeDrag) => {
      if (carriesFiles(event)) event.preventDefault();
      depth = 0;
      setPageDrag(false);
      setOver(false);
    };
    window.addEventListener('dragenter', enter);
    window.addEventListener('dragleave', leave);
    window.addEventListener('dragover', hover);
    window.addEventListener('drop', end);
    return () => {
      window.removeEventListener('dragenter', enter);
      window.removeEventListener('dragleave', leave);
      window.removeEventListener('dragover', hover);
      window.removeEventListener('drop', end);
    };
  }, [enabled]);

  const handlers = {
    onDragOver: (event: DragEvent) => {
      if (!carriesFiles(event)) return;
      event.preventDefault();
      setOver(true);
    },
    onDragLeave: (event: DragEvent) => {
      if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(false);
    },
    onDrop: (event: DragEvent) => {
      if (!carriesFiles(event)) return;
      event.preventDefault();
      setOver(false);
      onFiles(Array.from(event.dataTransfer.files));
    },
  };
  return { pageDrag: pageDrag && enabled, over: over && enabled, handlers };
}

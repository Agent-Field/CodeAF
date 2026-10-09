import { useEffect, useRef, useState } from 'react';
import { Button, IconButton } from '../../components/ui';
import { isMac } from '../../design/keyboard';
import './task-view.css';

export type BreadcrumbSegment = { id: string | null; label: string };

const COLLAPSE_BELOW = 480;

/** Cmd+[ / Cmd+] on Mac, Ctrl+[ / Ctrl+] elsewhere. */
export function historyDirection(event: Pick<KeyboardEvent, 'code' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>): -1 | 0 | 1 {
  const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  if (!primary || event.altKey || event.shiftKey) return 0;
  if (event.code === 'BracketLeft') return -1;
  return event.code === 'BracketRight' ? 1 : 0;
}

export function useHistoryKeys(onBack: () => void, onForward: () => void) {
  const handlers = useRef({ onBack, onForward });
  handlers.current = { onBack, onForward };
  useEffect(() => {
    const listen = (event: KeyboardEvent) => {
      const direction = historyDirection(event);
      if (!direction) return;
      event.preventDefault();
      if (direction < 0) handlers.current.onBack();
      else handlers.current.onForward();
    };
    window.addEventListener('keydown', listen);
    return () => window.removeEventListener('keydown', listen);
  }, []);
}

function useNarrow(ref: React.RefObject<HTMLElement | null>) {
  const [narrow, setNarrow] = useState(false);
  useEffect(() => {
    const node = ref.current;
    if (!node || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(([entry]) => setNarrow(entry.contentRect.width < COLLAPSE_BELOW));
    observer.observe(node);
    return () => observer.disconnect();
  }, [ref]);
  return narrow;
}

type Props = {
  segments: BreadcrumbSegment[];
  onNavigate: (id: string | null) => void;
  canBack: boolean;
  canForward: boolean;
  onBack: () => void;
  onForward: () => void;
};

function visibleSegments(segments: BreadcrumbSegment[], narrow: boolean) {
  const last = segments.length - 1;
  return segments.map((segment, index) => ({ segment, index, collapsed: narrow && index > 0 && index < last }));
}

export function Breadcrumb({ segments, onNavigate, canBack, onBack }: Props) {
  const ref = useRef<HTMLElement>(null);
  const narrow = useNarrow(ref);
  const last = segments.length - 1;
  let ellipsisShown = false;
  return (
    <nav className="breadcrumb" aria-label="Breadcrumb" ref={ref}>
      <IconButton className="breadcrumb-back" icon="back" iconSize="sm" label="Back" disabled={!canBack} onClick={onBack} />
      <ol className="breadcrumb-list">
        {visibleSegments(segments, narrow).map(({ segment, index, collapsed }) => {
          if (collapsed && ellipsisShown) return null;
          const showEllipsis = collapsed;
          ellipsisShown = ellipsisShown || collapsed;
          return (
            <li key={`${index}:${segment.id ?? 'root'}`} className="breadcrumb-item" data-current={index === last || undefined}>
              {index > 0 && <span className="breadcrumb-sep" aria-hidden="true">/</span>}
              {showEllipsis ? (
                <span className="breadcrumb-ellipsis">…</span>
              ) : index === last ? (
                <span className="breadcrumb-current" aria-current="page" title={segment.label}>{segment.label}</span>
              ) : (
                <Button className="breadcrumb-link" title={segment.label} onClick={() => onNavigate(segment.id)}>
                  {segment.label}
                </Button>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

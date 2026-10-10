import { useEffect, useRef } from 'react';
import { Button, Icon } from '../../components/ui';
import { isMac } from '../../design/keyboard';
import './task-view.css';
import '../focus-history/back-chip.css';

export type BreadcrumbSegment = { id: string | null; label: string };

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

type Props = {
  segments: BreadcrumbSegment[];
  onNavigate: (id: string | null) => void;
  canBack: boolean;
  canForward: boolean;
  onBack: () => void;
  onForward: () => void;
};

/** The immediate parent is the return affordance; older ancestors stay in keyboard history. */
export function Breadcrumb({ segments, canBack, onBack }: Props) {
  const current = segments[segments.length - 1];
  const parent = segments[segments.length - 2];
  if (!current) return null;
  return (
    <nav className="breadcrumb back-header" aria-label="Breadcrumb">
      {parent && canBack && <>
        <Button className="back-header-parent" onClick={onBack} title={parent.label}>
          <Icon name="back" />
          <span className="back-header-label">{parent.label}</span>
        </Button>
        <span className="back-header-separator" aria-hidden="true">/</span>
      </>}
      <span className="back-header-current" aria-current="page" title={current.label}>{current.label}</span>
    </nav>
  );
}

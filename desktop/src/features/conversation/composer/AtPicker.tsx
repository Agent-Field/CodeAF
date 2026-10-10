// The @ file list. Conversation 1e names the gesture (I-C1e-64/65) and does not draw the
// popover, so the list follows the menu (C-MENU-2/3) and the popover law (I-C1f-26):
// paper, a fill on hover, and it goes away when its anchor scrolls. What lands in the
// field is decided by atQuery (AT1): the relative path, as literal text.

import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { Button, Icon } from '../../../components/ui';
import design from '../../../design/tokens.json';
import { findEngineFiles } from '../../chat/engine-client';
import { activeAt, applyAtPath, rankAtFiles, type AtFile, type AtQuery } from './atQuery';
import './at-picker.css';

export type { AtFile };

type AtPickerProps = {
  anchor: RefObject<HTMLElement | null>;
  files: readonly AtFile[];
  active: number;
  listId: string;
  onHighlight: (index: number) => void;
  onPick: (file: AtFile) => void;
  onClose: () => void;
};

function tokenKey(token: AtQuery): string {
  return `${token.at}:${token.query}`;
}

function optionId(listId: string, index: number): string {
  return `${listId}-option-${index}`;
}

/**
 * Files matching the query, best first. An empty query is not a search: the engine
 * matches nothing for it, and a miss or an error draws nothing (emptiness law).
 * Older answers never overwrite a newer query.
 */
function useAtMatches(sessionId: string | undefined, query: string | null): AtFile[] {
  const [files, setFiles] = useState<AtFile[]>([]);
  useEffect(() => {
    if (!sessionId || !query) {
      setFiles([]);
      return;
    }
    let current = true;
    setFiles([]);
    const timer = window.setTimeout(() => {
      findEngineFiles(sessionId, query)
        .then(found => { if (current) setFiles(rankAtFiles(found.files ?? [], query)); })
        .catch(() => { if (current) setFiles([]); });
    }, design.interaction.atPickerDebounceMs);
    return () => { current = false; window.clearTimeout(timer); };
  }, [sessionId, query]);
  return files;
}

/** Keeps the popover above the composer, and as wide as the composer once the window is at the small breakpoint. */
function usePlacement(anchor: RefObject<HTMLElement | null>, popover: RefObject<HTMLDivElement | null>) {
  useLayoutEffect(() => {
    const place = () => {
      const box = anchor.current?.getBoundingClientRect();
      const node = popover.current;
      if (!box || !node || box.width < 1) return;
      const pad = design.overlay.collisionPadding;
      const preferred = Number.parseFloat(design.foundation['at-picker-width']);
      const narrow = window.innerWidth <= design.breakpoints.small;
      const width = Math.round(narrow ? box.width : Math.min(box.width, preferred));
      let left = box.left;
      if (left + width > window.innerWidth - pad) left = window.innerWidth - pad - width;
      if (left < pad) left = pad;
      node.style.setProperty('--at-picker-left', `${Math.round(left)}px`);
      node.style.setProperty('--at-picker-bottom', `${Math.round(window.innerHeight - box.top + design.overlay.sideOffset)}px`);
      node.style.setProperty('--at-picker-span', `${width}px`);
    };
    place();
    window.addEventListener('resize', place);
    return () => window.removeEventListener('resize', place);
  });
}

/** A scroll that is not the list's own closes it (1f). A press inside the composer does not: that is the caret moving. */
function useDismiss(popover: RefObject<HTMLDivElement | null>, anchor: RefObject<HTMLElement | null>, onClose: () => void) {
  useEffect(() => {
    const outside = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (popover.current?.contains(target) || anchor.current?.contains(target)) return;
      onClose();
    };
    const scrolled = (event: Event) => {
      if (event.target instanceof Node && popover.current?.contains(event.target)) return;
      onClose();
    };
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('scroll', scrolled, true);
    return () => {
      document.removeEventListener('pointerdown', outside, true);
      window.removeEventListener('scroll', scrolled, true);
    };
  }, [popover, anchor, onClose]);
}

/** The directory, both ends kept: the head gives up the middle when the row runs out of room. */
function Dir({ dir }: { dir: string }) {
  const mid = Math.ceil(dir.length / 2);
  return (
    <span className="at-picker-dir">
      <span className="at-picker-dir-head">{dir.slice(0, mid)}</span>
      <span className="at-picker-dir-tail">{dir.slice(mid)}</span>
    </span>
  );
}

/**
 * The list above the composer. Nothing is drawn when there is nothing to choose:
 * the caller passes an empty list for a miss, an error, a bare @, or no session.
 * The open list is its own component so its scroll listener exists only while it is up.
 */
export function AtPicker(props: AtPickerProps) {
  if (props.files.length === 0) return null;
  return <AtPickerOpen {...props} />;
}

function AtPickerOpen({ anchor, files, active, listId, onHighlight, onPick, onClose }: AtPickerProps) {
  const popover = useRef<HTMLDivElement>(null);
  usePlacement(anchor, popover);
  useDismiss(popover, anchor, onClose);
  useEffect(() => {
    // Scroll the list itself. scrollIntoView would move an ancestor, and that scroll is what closes the list.
    const list = popover.current;
    const row = list?.querySelector<HTMLElement>('[aria-selected="true"]');
    if (!list || !row) return;
    const top = row.offsetTop;
    const bottom = top + row.offsetHeight;
    if (top < list.scrollTop) list.scrollTop = top;
    else if (bottom > list.scrollTop + list.clientHeight) list.scrollTop = bottom - list.clientHeight;
  }, [active, files]);
  return createPortal(
    <div
      ref={popover}
      id={listId}
      className="at-picker"
      role="listbox"
      aria-label="Files"
      onMouseDown={event => event.preventDefault()}
    >
      {files.map((file, index) => (
        <Button
          key={file.path}
          id={optionId(listId, index)}
          role="option"
          aria-selected={index === active}
          aria-label={file.path}
          className="at-picker-row"
          onMouseEnter={() => onHighlight(index)}
          onClick={() => onPick(file)}
        >
          <Icon name="file" size="sm" />
          <span className="at-picker-name">{file.name}</span>
          {file.dir ? <Dir dir={file.dir} /> : null}
        </Button>
      ))}
    </div>,
    document.body,
  );
}

export type AtPickerController = {
  open: boolean;
  files: readonly AtFile[];
  active: number;
  listId: string;
  activeId?: string;
  sync: (node: HTMLTextAreaElement | null) => void;
  highlight: (index: number) => void;
  pick: (file?: AtFile) => void;
  close: () => void;
  onKeyDown: (event: KeyboardEvent<HTMLTextAreaElement>) => boolean;
};

/**
 * Which @ the focused field is in, and the matches for it.
 * Esc dismisses that exact query until the person changes it. A composer with no
 * session cannot ask the engine, so it stays quiet rather than showing a broken list.
 */
export function useAtPicker(
  field: RefObject<HTMLTextAreaElement | null>,
  sessionId: string | undefined,
  draft: string,
  onDraft: (value: string) => void,
  placeCaret: (caret: number) => void,
): AtPickerController {
  const listId = useId();
  const [token, setToken] = useState<AtQuery | null>(null);
  const [dismissed, setDismissed] = useState<string | null>(null);
  const [active, setActive] = useState(0);
  const tokenRef = useRef(token);
  tokenRef.current = token;

  const sync = useCallback((node: HTMLTextAreaElement | null) => {
    setToken(current => {
      if (!node || node.disabled || document.activeElement !== node) return null;
      const next = activeAt(node.value, node.selectionStart);
      if (!next) return null;
      if (current && current.at === next.at && current.end === next.end && current.query === next.query) return current;
      return next;
    });
  }, []);

  useEffect(() => { sync(field.current); }, [draft, sessionId, field, sync]);

  useEffect(() => {
    if (!token || !dismissed) return;
    if (tokenKey(token) !== dismissed) setDismissed(null);
  }, [token, dismissed]);

  const shown = token && token.query && dismissed !== tokenKey(token) ? token : null;
  const files = useAtMatches(sessionId, shown ? shown.query : null);
  const listKey = files.map(file => file.path).join('\0');
  useEffect(() => { setActive(0); }, [listKey]);
  const index = files.length === 0 ? 0 : Math.min(active, files.length - 1);

  const close = useCallback(() => {
    const current = tokenRef.current;
    if (current) setDismissed(tokenKey(current));
  }, []);

  const highlight = useCallback((at: number) => setActive(at), []);

  const pick = useCallback((file?: AtFile) => {
    const node = field.current;
    const chosen = file ?? files[index];
    if (!node || !chosen) return;
    const next = applyAtPath(node.value, node.selectionStart, chosen.path);
    if (!next) return;
    setToken(null);
    onDraft(next.text);
    placeCaret(next.caret);
  }, [field, files, index, onDraft, placeCaret]);

  const onKeyDown = useCallback((event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (!files.length) return false;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      setActive(current => {
        const at = Math.min(current, files.length - 1);
        const next = at + (event.key === 'ArrowDown' ? 1 : -1);
        if (next < 0 || next >= files.length) return at;
        return next;
      });
      return true;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      close();
      return true;
    }
    if (event.key === 'Tab' || (event.key === 'Enter' && !event.shiftKey)) {
      event.preventDefault();
      pick();
      return true;
    }
    return false;
  }, [files, close, pick]);

  return {
    open: files.length > 0,
    files,
    active: index,
    listId,
    activeId: files.length ? optionId(listId, index) : undefined,
    sync,
    highlight,
    pick,
    close,
    onKeyDown,
  };
}

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import design from '../../design/tokens.json';
import type { ConversationModel } from './types';

// Within this distance of the end the reader counts as "at the bottom" (design 1f, Anchor).
const SLACK = parseFloat(design.foundation['scroll-anchor']);

const distanceToEnd = (element: HTMLElement) => element.scrollHeight - element.clientHeight - element.scrollTop;

/** Changes when something new is said, not when the reader opens a disclosure. */
export function contentSignature(model: ConversationModel): string {
  const last = model.turns[model.turns.length - 1];
  const tail = last?.blocks[last.blocks.length - 1];
  const size = tail && 'text' in tail ? tail.text.length : tail?.kind === 'work' ? tail.steps.length : 0;
  return [model.turns.length, last?.blocks.length ?? 0, size, model.questions.length].join(':');
}

/**
 * Follows new content only while the reader is at the bottom; otherwise it
 * reports that something arrived below so the view can offer a jump. `scrolled`
 * is true once the top edge has content above it, which turns the top fade on.
 */
export function useStickToBottom(scroller: RefObject<HTMLElement | null>, content: RefObject<HTMLElement | null>, signature: string, pageKey?: string) {
  const atBottom = useRef(true);
  const onPage = useRef(Boolean(pageKey));
  onPage.current = Boolean(pageKey);
  const [behind, setBehind] = useState(false);
  const [away, setAway] = useState(false);
  const [unanchored, setUnanchored] = useState(false);
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const element = scroller.current;
    const inner = content.current;
    if (!element || !inner) return;
    const onScroll = () => {
      atBottom.current = !onPage.current && distanceToEnd(element) <= SLACK;
      setAway(!atBottom.current && !onPage.current);
      if (atBottom.current) setBehind(false);
      setUnanchored(!atBottom.current);
      setScrolled(element.scrollTop > 0);
    };
    const follow = () => {
      if (atBottom.current) element.scrollTop = element.scrollHeight;
    };
    const observer = new ResizeObserver(follow);
    observer.observe(inner);
    element.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      observer.disconnect();
      element.removeEventListener('scroll', onScroll);
    };
  }, [scroller, content]);

  useLayoutEffect(() => {
    const element = scroller.current;
    if (!element) return;
    if (atBottom.current) element.scrollTop = element.scrollHeight;
    else setBehind(true);
  }, [scroller, signature]);

  // A task page is read from its top; the conversation itself returns to its end.
  useLayoutEffect(() => {
    const element = scroller.current;
    if (!element || pageKey === undefined) return;
    atBottom.current = !pageKey;
    element.scrollTop = pageKey ? 0 : element.scrollHeight;
  }, [scroller, pageKey]);

  const jump = useCallback(() => {
    const element = scroller.current;
    if (!element) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    element.scrollTo({ top: element.scrollHeight, behavior: reduced ? 'auto' : 'smooth' });
    atBottom.current = true;
    setBehind(false);
    setAway(false);
    setUnanchored(false);
  }, [scroller]);

  return { behind, away, unanchored, scrolled, jump };
}

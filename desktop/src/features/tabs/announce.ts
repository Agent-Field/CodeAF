import { useCallback, useEffect, useState, type RefObject } from 'react';

const announcementEvent = 'codeaf:strip-announcement';

/** A bubbling event keeps an announcement in the strip that owns the focused control. */
export function announce(control: HTMLElement, message: string) {
  control.dispatchEvent(new CustomEvent<string>(announcementEvent, { bubbles: true, detail: message }));
}

/** Tab and group moves share one mounted polite region, without a visible notification. */
export function useStripAnnouncement(strip: RefObject<HTMLElement | null>) {
  const [note, setNote] = useState({ text: '', revision: 0 });
  const publish = useCallback((text: string) => setNote(previous => ({ text, revision: previous.revision + 1 })), []);
  useEffect(() => {
    const root = strip.current;
    if (!root) return;
    const receive = (event: Event) => publish((event as CustomEvent<string>).detail);
    root.addEventListener(announcementEvent, receive);
    return () => root.removeEventListener(announcementEvent, receive);
  }, [strip, publish]);
  // The revision replaces the text node even when a later move repeats the same words.
  return [note, publish] as const;
}

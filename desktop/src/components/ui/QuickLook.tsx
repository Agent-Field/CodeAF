import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react';
import { PlaceSwatch, type TintName } from '../../features/places/components/PlaceSwatch';
import './quick-look.css';

type QuickLookProps = {
  open: boolean;
  /** Esc, the scrim and Space all end here; the owner stops passing open. */
  onClose: () => void;
  title: string;
  /** The place's tint, drawn as the header swatch. No tint draws no swatch. */
  tint?: TintName;
  children?: ReactNode;
  /** The one primary action, and any quiet ones beside it. Nothing given draws no footer. */
  footer?: ReactNode;
};

/** Where Space is a character, not a command (I-IKY-23): inside a text field it types; a focused button keeps its own Space activation. */
function spaceBelongsToTarget(target: EventTarget) {
  if (!(target instanceof HTMLElement)) return false;
  return target.isContentEditable || target.matches('input:not([type=checkbox]):not([type=radio]), textarea, select, button, a[href], [role=button]');
}

/** The Quick Look sheet shell (C-OVL-4). A native modal dialog supplies the focus trap, Esc, the scrim and focus return to the opener;
 * the places area supplies the content. Space closes it, matching the Space that opened it. */
export function QuickLook({ open, onClose, title, tint, children, footer }: QuickLookProps) {
  const dialog = useRef<HTMLDialogElement>(null);
  const wasOpen = useRef(open);
  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) element.showModal();
    // Closing through the dialog lets the browser hand focus back to the opener before the owner reacts.
    if (!open && element.open) element.close();
    wasOpen.current = open;
  }, [open]);
  // The close event also fires for our own close(); only a close the person caused (Esc) is the owner's to hear.
  const closed = () => { if (wasOpen.current) onClose(); };
  const keyDown = (event: KeyboardEvent) => {
    if (event.key !== ' ' || spaceBelongsToTarget(event.target)) return;
    event.preventDefault();
    onClose();
  };
  return <dialog ref={dialog} className="quick-look" aria-labelledby="quick-look-title" onClose={closed} onKeyDown={keyDown}
    onClick={event => { if (event.target === dialog.current) onClose(); }}>
    <header className="quick-look-head">
      {tint && <PlaceSwatch tint={tint} role="sheet"/>}
      <h2 id="quick-look-title" className="quick-look-title">{title}</h2>
      <span className="quick-look-hint">Space to close</span>
    </header>
    <div className="quick-look-body">{children}</div>
    {footer && <footer className="quick-look-foot">{footer}</footer>}
  </dialog>;
}

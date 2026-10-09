import { useEffect, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

type OverlayProps = { label: string; variant: 'sheet' | 'lightbox'; onClose: () => void; onKeyDown?: (event: React.KeyboardEvent) => void; children: ReactNode };

/**
 * A native modal dialog: focus trap, Escape and top layer come from the platform.
 * Closing returns focus to whatever opened it.
 */
export function Overlay({ label, variant, onClose, onKeyDown, children }: OverlayProps) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const node = dialog.current;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    if (node && !node.open) node.showModal();
    return () => {
      if (node?.open) node.close();
      if (opener?.isConnected) opener.focus();
    };
  }, []);
  return createPortal(
    <dialog
      ref={dialog}
      className={`asset-overlay asset-overlay-${variant}`}
      aria-label={label}
      onCancel={event => {
        event.preventDefault();
        onClose();
      }}
      onClick={event => {
        if (event.target === event.currentTarget) onClose();
      }}
      onKeyDown={onKeyDown}
    >
      {children}
    </dialog>,
    document.body,
  );
}

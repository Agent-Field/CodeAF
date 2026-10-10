import { useEffect, useRef, useState } from 'react';
import { Button, Icon } from '../../components/ui';
import { isMac } from '../../design/keyboard';
import './back-chip.css';

export type BackChipProps = {
  parentName: string;
  onBack: () => void;
  onDismiss?: () => void;
};

/** Mount in TabStrip's leading slot for an unsolicited jump; remount for each new jump. */
export function BackChip({ parentName, onBack, onDismiss }: BackChipProps) {
  const ref = useRef<HTMLButtonElement>(null);
  const [dismissed, setDismissed] = useState(false);
  useEffect(() => {
    if (dismissed || !parentName) return;
    const dismiss = (event: Event) => {
      // Capture sees actions even when another control stops propagation.
      if (event.composedPath().includes(ref.current as EventTarget)) return;
      setDismissed(true);
      onDismiss?.();
    };
    window.addEventListener('click', dismiss, true);
    window.addEventListener('keydown', dismiss, true);
    return () => {
      window.removeEventListener('click', dismiss, true);
      window.removeEventListener('keydown', dismiss, true);
    };
  }, [dismissed, parentName, onDismiss]);
  if (dismissed || !parentName) return null;
  return <Button ref={ref} variant="raised" className="back-chip" onClick={onBack} title={`Back to ${parentName}`}>
    <Icon name="back" />
    <span className="back-chip-label">Back to {parentName}</span>
    <span className="back-chip-shortcut" aria-hidden="true">{isMac ? '⌘[' : 'Ctrl+['}</span>
  </Button>;
}

import { useEffect, useRef, useState } from 'react';
import design from '../../design/tokens.json';
import { IconButton, type IconButtonSize } from './Button';
import type { IconSize } from './Icon';

export type CopyButtonProps = { text: string; label?: string; className?: string; size?: IconButtonSize; iconSize?: IconSize };

/** Copies text; a failed write simply never shows the copied state. */
export function CopyButton({ text, label = 'Copy', className = '', size, iconSize = 'sm' }: CopyButtonProps) {
 const [copied, setCopied] = useState(false);
 const timer = useRef<number>(0);
 useEffect(() => () => window.clearTimeout(timer.current), []);
 const copy = async () => {
  try {
   await navigator.clipboard.writeText(text);
  } catch {
   return;
  }
  setCopied(true);
  window.clearTimeout(timer.current);
  timer.current = window.setTimeout(() => setCopied(false), design.interaction.copiedFeedbackMs);
 };
 return <span className={`copy-button ${className}`} data-copied={copied}>
  <span className="copy-button-status" role="status">{copied ? 'Copied' : ''}</span>
  <IconButton label={label} icon={copied ? 'check' : 'copy'} size={size} iconSize={iconSize} onClick={copy}/>
 </span>;
}

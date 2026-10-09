import type { ButtonHTMLAttributes, HTMLAttributes } from 'react';

/** The 20px inline pill on field (file and link chips compose it). Interactive chips are buttons. */
export function Chip({ className = '', muted = false, ...props }: HTMLAttributes<HTMLSpanElement> & { muted?: boolean }) {
 return <span {...props} className={`chip ${className}`} data-muted={muted || undefined}/>;
}
export function ChipButton({ className = '', type = 'button', ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
 return <button {...props} type={type} className={`chip chip-button ${className}`}/>;
}

/** Shared tags: plain suggested labels, pills, irreversible warnings and key hints. */
export type TagTone = 'accent' | 'neutral' | 'danger' | 'key' | 'plain';
export function Tag({ tone = 'neutral', className = '', ...props }: HTMLAttributes<HTMLSpanElement> & { tone?: TagTone }) {
 return <span {...props} className={`tag ${className}`} data-tone={tone}/>;
}

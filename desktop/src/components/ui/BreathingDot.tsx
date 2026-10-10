import type { HTMLAttributes } from 'react';
import './motion.css';

/**
 * The 6px accent dot beside the conversation header's "N running".
 * It is the only dot that breathes. The count words are the name, so this
 * mark stays out of the accessibility tree. Reduced motion leaves it still.
 */
export function BreathingDot({ className, ...props }: HTMLAttributes<HTMLSpanElement>) {
 return <span {...props} className={['cf-breathe', className].filter(Boolean).join(' ')} aria-hidden="true"/>;
}

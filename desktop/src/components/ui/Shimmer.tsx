import type { HTMLAttributes } from 'react';
import './motion.css';

/**
 * A quiet sweep of ink across ink-2, for the one live line of work.
 * Settled words pass active as false: the wrapper then adds no paint, so the
 * row's own colour shows through. Reduced motion keeps the words in ink-2.
 */
export function Shimmer({ active = false, className, children, ...props }: HTMLAttributes<HTMLSpanElement> & { active?: boolean }) {
 const classes = [active ? 'cf-shimmer' : '', className].filter(Boolean).join(' ');
 return <span {...props} className={classes || undefined} data-shimmer={active ? 'live' : 'still'}>{children}</span>;
}

import type { HTMLAttributes } from 'react';

/** A 30px hoverable row: rest none, hover field, selected field. Its RowActions stay hidden until
 * the row is hovered or holds focus, and are always visible on touch. */
export function Row({ selected = false, className = '', ...props }: HTMLAttributes<HTMLDivElement> & { selected?: boolean }) {
 return <div {...props} className={`row ${className}`} data-selected={selected || undefined}/>;
}
export function RowActions({ className = '', ...props }: HTMLAttributes<HTMLSpanElement>) {
 return <span {...props} className={`row-actions ${className}`}/>;
}

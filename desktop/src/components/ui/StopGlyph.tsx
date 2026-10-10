import type { HTMLAttributes } from 'react';
import './stop-glyph.css';

/** Icon's sm and md boxes. The square stays 10px inside either, so the two line up. */
export type StopGlyphSize = 'sm' | 'md';

/**
 * The filled square that stands in for lucide's square, which the animated set does not have.
 * Send becomes Stop, and holding ⌥ on a running tab's close, both draw this mark.
 * Those callers place it. The mark only occupies Icon's box and stays out of the accessibility tree.
 */
export function StopGlyph({ size = 'md', className, ...props }: HTMLAttributes<HTMLSpanElement> & { size?: StopGlyphSize }) {
 return <span {...props} className={['stop-glyph', className].filter(Boolean).join(' ')} data-size={size} aria-hidden="true">
  <span className="stop-glyph-mark"/>
 </span>;
}

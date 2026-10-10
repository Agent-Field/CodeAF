import { useLayoutEffect, useState, type RefObject } from 'react';

/**
 * What `useTitleOverflow` measures. `fade` is 0 when the title is not masked (a place Home
 * sizes to its name). A positive fade is the masked tail: text that sits in it is not readable.
 */
export type TitleBox = { clientWidth: number; scrollWidth: number; textWidth: number; fade: number };

/**
 * The name is not fully on the chip. Icon-only tabs (pinned, compressed) pass `hidden`:
 * there is no title box, so the name is overflow. A box that has not been laid out yet
 * (`clientWidth` 0) is not overflow — the next measure decides.
 */
export function titleOverflows(box: TitleBox | null, hidden: boolean): boolean {
  if (hidden) return true;
  if (!box || box.clientWidth <= 0) return false;
  // One pixel of slack: subpixel rounding must not invent a tooltip for a title that fits.
  if (box.scrollWidth > box.clientWidth + 1) return true;
  if (box.fade <= 0) return false;
  return box.textWidth > box.clientWidth - box.fade + 1;
}

/** Reads the title span. An unmasked title reports fade 0 so a Home name that fits is not a cut. */
function readBox(node: HTMLElement): TitleBox {
  const style = getComputedStyle(node);
  const prefixed = (style as CSSStyleDeclaration & { webkitMaskImage?: string }).webkitMaskImage;
  const mask = style.maskImage && style.maskImage !== 'none' ? style.maskImage : prefixed && prefixed !== 'none' ? prefixed : 'none';
  const fade = mask === 'none' ? 0 : Number.parseFloat(style.getPropertyValue('--tab-title-fade')) || 0;
  const range = node.ownerDocument.createRange();
  range.selectNodeContents(node);
  const textWidth = range.getBoundingClientRect().width;
  return { clientWidth: node.clientWidth, scrollWidth: node.scrollWidth, textWidth, fade };
}

/**
 * Whether this tab's title is truncated. `hidden` is the icon-only chip (pinned or compressed):
 * the title span is not mounted, and the name can only be read from the tooltip or a preview.
 * Remeasures when the span changes size, and once fonts finish, because a late font reflows the cut.
 */
export function useTitleOverflow(titleRef: RefObject<HTMLElement | null>, hidden: boolean): boolean {
  const [overflow, setOverflow] = useState(hidden);
  useLayoutEffect(() => {
    let cancel = false;
    const measure = () => { if (!cancel) setOverflow(titleOverflows(titleRef.current ? readBox(titleRef.current) : null, hidden)); };
    measure();
    const node = titleRef.current;
    if (!node || hidden) return () => { cancel = true; };
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    const fonts = node.ownerDocument.fonts;
    if (fonts) void fonts.ready.then(measure);
    return () => { cancel = true; observer.disconnect(); };
  }, [titleRef, hidden]);
  return overflow;
}

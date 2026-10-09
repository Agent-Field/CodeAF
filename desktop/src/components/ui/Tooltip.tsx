import { useEffect, useId, useLayoutEffect, useRef, useState, type FocusEvent, type PointerEvent } from 'react';
import { createPortal } from 'react-dom';
import design from '../../design/tokens.json';

// Once one tooltip has shown, the next one opens at once while the pointer moves along a toolbar.
let warmUntil = 0;
const padding = design.overlay.collisionPadding;
const offset = design.overlay.sideOffset;

/** Places the tooltip under its trigger, flipping above when there is no room. Geometry is measured, not designed. */
function place(tip: HTMLElement, anchor: DOMRect) {
 const width = tip.offsetWidth, height = tip.offsetHeight;
 const left = Math.min(Math.max(anchor.left + anchor.width / 2 - width / 2, padding), window.innerWidth - width - padding);
 const below = anchor.bottom + offset;
 const top = below + height + padding > window.innerHeight ? anchor.top - offset - height : below;
 tip.style.setProperty('translate', `${Math.round(left)}px ${Math.round(top)}px`);
}

type TriggerHandlers<E extends HTMLElement> = {
 onPointerEnter?: (event: PointerEvent<E>) => void;
 onPointerLeave?: (event: PointerEvent<E>) => void;
 onPointerDown?: (event: PointerEvent<E>) => void;
 onFocus?: (event: FocusEvent<E>) => void;
 onBlur?: (event: FocusEvent<E>) => void;
};

type TooltipOptions = {
 /** The text says more than the trigger's accessible name (a full path behind a file name), so it is also the
  * trigger's description. A tooltip that only repeats the name stays undescribed, or a reader hears it twice. */
 describe?: boolean;
 /** A muted key hint drawn after the text (⌘W). Visual only: the trigger's own name or menu carries the key. */
 shortcut?: string;
};

/** The designer's tooltip: icon-only buttons and truncated text only. It appears after the
 * delay (instantly while another is warm), uses sh-2 and 11px text, never on touch, only for
 * keyboard focus, and goes away on scroll, press or Escape. The trigger keeps its accessible name. */
export function useTooltip<E extends HTMLElement = HTMLElement>(text: string, handlers: TriggerHandlers<E> = {}, options: TooltipOptions = {}) {
 const [anchor, setAnchor] = useState<{ rect: DOMRect; container: Element } | null>(null);
 const timer = useRef(0);
 const tip = useRef<HTMLSpanElement>(null);
 const id = useId();
 const hide = () => { window.clearTimeout(timer.current); setAnchor(current => { if (current) warmUntil = Date.now() + design.interaction.previewCloseDelay; return null; }); };
 const show = (element: HTMLElement) => {
  const open = () => setAnchor({ rect: element.getBoundingClientRect(), container: element.closest('dialog') ?? document.body });
  window.clearTimeout(timer.current);
  if (Date.now() < warmUntil) open(); else timer.current = window.setTimeout(open, design.interaction.tooltipOpenDelay);
 };
 useLayoutEffect(() => { if (anchor && tip.current) place(tip.current, anchor.rect); }, [anchor]);
 useEffect(() => {
  if (!anchor) return;
  const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') hide(); };
  window.addEventListener('scroll', hide, true); window.addEventListener('keydown', escape);
  return () => { window.removeEventListener('scroll', hide, true); window.removeEventListener('keydown', escape); };
 }, [anchor]);
 useEffect(() => () => window.clearTimeout(timer.current), []);
 const props: Required<TriggerHandlers<E>> = {
  onPointerEnter: event => { handlers.onPointerEnter?.(event); if (event.pointerType !== 'touch') show(event.currentTarget); },
  onPointerLeave: event => { handlers.onPointerLeave?.(event); hide(); },
  onPointerDown: event => { handlers.onPointerDown?.(event); hide(); },
  onFocus: event => { handlers.onFocus?.(event); if (event.currentTarget.matches(':focus-visible')) show(event.currentTarget); },
  onBlur: event => { handlers.onBlur?.(event); hide(); },
 };
 // A described trigger points at the same id whether or not the tooltip is drawn: the hidden copy carries the
 // description while it is closed, so a keyboard focus hears it at once instead of after the open delay.
 const describe = !!options.describe && !!text;
 const shortcut = options.shortcut;
 const element = anchor && text
  ? createPortal(<span ref={tip} id={id} role="tooltip" className="tooltip">{text}{shortcut && <span className="tooltip-shortcut">{shortcut}</span>}</span>, anchor.container)
  : describe ? <span id={id} hidden>{text}</span> : null;
 return { props: describe ? { ...props, 'aria-describedby': id } : props, element };
}

/** Single-line text that ellipsizes; the full text shows as a tooltip only when it is actually cut. */
export function TruncatedText({ text, className = '' }: { text: string; className?: string }) {
 const [cut, setCut] = useState(false);
 const tooltip = useTooltip(cut ? text : '', { onPointerEnter: event => setCut(event.currentTarget.scrollWidth > event.currentTarget.clientWidth) });
 return <span className={`truncated-text ${className}`} {...tooltip.props}>{text}{tooltip.element}</span>;
}

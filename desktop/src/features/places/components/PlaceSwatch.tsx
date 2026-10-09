import './places-components.css';

/** The fixed palette of six (Foundations "Place tints"). Graphite is the neutral: Now, and places with no tint chosen. */
export const placeTints = ['tide', 'iris', 'rose', 'sand', 'sage', 'graphite'] as const;
export type TintName = typeof placeTints[number];
/** The five a person can pick (Places 8f, 9d): Graphite is never offered in a picker. */
export const choosableTints: readonly TintName[] = placeTints.filter(tint => tint !== 'graphite');
export const tintLabel: Record<TintName, string> = { tide: 'Tide', iris: 'Iris', rose: 'Rose', sand: 'Sand', sage: 'Sage', graphite: 'Graphite' };

/** Where a swatch sits fixes its size (Places helpers sw(), Components): rail 10, tile 12, sheet 14, title 16, card 8,
 * and the two picker sizes, choice 12 (inline create) and menu 16 (the place menu's Tint row). */
export type SwatchRole = 'rail' | 'tile' | 'sheet' | 'title' | 'card' | 'choice' | 'menu';

type PlaceSwatchProps = {
  tint: TintName;
  role?: SwatchRole;
  /** Picker only: the chosen swatch carries the ring that separates it from the surface. */
  selected?: boolean;
  /** Picker only: the swatches that are not chosen sit at half strength. */
  dimmed?: boolean;
  /** Names the tint to a screen reader. Omit when the place's name sits right beside it: the swatch is identity, not status. */
  label?: string;
};

/** A place's tint square. Colour is a token per tint and per theme; the radius is 0.3 of the side. */
export function PlaceSwatch({ tint, role = 'tile', selected, dimmed, label }: PlaceSwatchProps) {
  return <span className="place-swatch" data-tint-name={tint} data-role={role} data-selected={selected || undefined} data-dimmed={dimmed || undefined}
    role={label ? 'img' : undefined} aria-label={label} aria-hidden={label ? undefined : true}/>;
}

import { Button, Icon } from '../../components/ui';
import { PlaceSwatch, placeTints, type TintName } from './components/PlaceSwatch';
import { usePlacesShell } from './shell/PlacesShell';
import '../focus-history/back-chip.css';

/**
 * The strip's way out of Focus (owner decision 2026-10-11): while the strip is filtered to a place, this chip names it
 * and clears the filter. It draws nothing when no place is focused, and the place's tint is only the 10px swatch, so
 * colour stays on a glyph. It shares the Back chip's geometry rather than adding a second chip shape.
 */
export function FocusChip() {
  const shell = usePlacesShell();
  const id = shell?.focus;
  if (!shell || !id) return null;
  const place = shell.index?.byId.get(id);
  const tint = (placeTints as readonly string[]).includes(place?.effectiveTint ?? '') ? place!.effectiveTint as TintName : 'graphite';
  const name = place?.name ?? 'this place';
  return <Button variant="raised" className="back-chip focus-chip" onClick={() => shell.setFocus(undefined)} title={`Clear Focus on ${name}`} aria-label={`Focus: ${name}. Clear`}>
    <PlaceSwatch tint={tint} role="rail"/>
    <span className="back-chip-label">Focus · {name}</span>
    <span className="back-chip-shortcut" aria-hidden="true"><Icon name="close" size="micro"/></span>
  </Button>;
}

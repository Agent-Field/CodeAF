import { SectionLabel } from '../../../components/ui';

/**
 * The "Since yesterday / Since last week" recap at the top of a Home (Places 8a, 9a, P-CMP-20). The label and the
 * words are the engine's; with no text there is nothing to draw, never a placeholder, and nothing here asks a model.
 */
export function SinceBlock({ recap }: { recap?: { label: string; text: string } }) {
  const label = recap?.label.trim(), text = recap?.text.trim();
  if (!label || !text) return null;
  return <section className="home-section home-recap" aria-label={label}>
    <SectionLabel>{label}</SectionLabel>
    <p className="home-recap-text">{text}</p>
  </section>;
}

import { SectionHeading, Text, ThemeSelect } from '../../components/ui';

/**
 * Appearance: the theme choice, applied at once and kept across reloads by the ThemeProvider. Reduce motion has no
 * control on purpose (I-IFL-26): it follows the operating system, so the section says so in one muted line.
 */
export function AppearanceSection() {
  return (
    <section className="models-section" aria-labelledby="settings-appearance">
      <SectionHeading id="settings-appearance">Appearance</SectionHeading>
      <ThemeSelect />
      <Text className="settings-note">Reduce motion follows your system setting.</Text>
    </section>
  );
}

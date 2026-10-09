/** The Settings tab kind's words: its title, the overview card's summary, and the icon the tab strip draws. */
export const SETTINGS_TAB_TITLE = 'Models';
export const SETTINGS_TAB_ICON = 'sliders';

const EFFORT_WORDS: Record<string, string> = { low: 'Low', medium: 'Medium', high: 'High' };
/** The word a person reads for an effort level; unknown levels read as nothing. */
export const effortWord = (effort: string | undefined): string => (effort ? EFFORT_WORDS[effort] ?? '' : '');

/**
 * "Pinned: GLM Flash, DS Flash, GLM 5.3. Default effort: Medium." The effort clause is left out while the
 * conversation role runs on the model's own effort, because there is then nothing to say.
 */
export function settingsSummary(view: { pinned: readonly { label: string }[]; conversationEffort?: string }): string {
  const pinned = view.pinned.length > 0 ? `Pinned: ${view.pinned.map(model => model.label).join(', ')}.` : '';
  const effort = effortWord(view.conversationEffort);
  return [pinned, effort ? `Default effort: ${effort}.` : ''].filter(Boolean).join(' ');
}

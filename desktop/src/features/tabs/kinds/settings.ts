import { SETTINGS_TAB_ICON } from '../../settings';
import { SettingsPane, SettingsPreview } from './SettingsPane';
import type { KindDef } from './slots';

/** Live. Backed by the engine's model roles and pinned models; the rail's Settings item opens or focuses this tab. */
export const settingsKind: KindDef = { kind: 'settings', label: 'Settings', icon: SETTINGS_TAB_ICON, backed: true, pane: SettingsPane, preview: SettingsPreview };

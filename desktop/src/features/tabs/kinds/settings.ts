import { placeholderPane } from './Placeholder';
import { PlainPreview } from '../preview/bodies';
import type { KindDef } from './slots';

/** Placeholder. Settings and tools have no tab surface yet. */
export const settingsKind: KindDef = { kind: 'settings', label: 'Settings', icon: 'settings', backed: false, pane: placeholderPane('Settings', 'settings'), preview: PlainPreview };

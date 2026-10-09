import { HomePane } from '../../places/shell/HomePane';
import { PlainPreview } from '../preview/bodies';
import type { KindDef } from './slots';

/**
 * A place's Home (Places 8a) or All places (8c): the tab whose `place` is the place id, or `root`. A place's Home is
 * the pinned first tab of its strip and never closes (reducers/home.ts). The engine's Places routes back it.
 */
export const homeKind: KindDef = { kind: 'home', label: 'Home', icon: 'layers', backed: true, pane: HomePane, preview: PlainPreview };

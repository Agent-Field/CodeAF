import { WebPane } from '../../web/WebPane';
import { WebPreview } from '../preview/bodies';
import type { KindDef } from './slots';

/** A page in a native child view (src-tauri/src/web.rs); outside the desktop app the pane says so and offers the browser. Its hover card and overview body are the shared preview's web card. */
export const webKind: KindDef = { kind: 'web', label: 'Web', icon: 'web', backed: true, pane: WebPane, preview: WebPreview };

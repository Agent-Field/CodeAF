import { WebPane } from '../../web/WebPane';
import { WebPreview } from '../preview/bodies';
import { nativeWebAvailable } from '../../../design/nativeWeb';
import type { KindDef } from './slots';

/** A page in a native child view (src-tauri/src/web.rs); outside the desktop app the pane says so and offers the browser. Backed only where native web exists (the desktop app); in a browser build the kind stays unbacked and every opener uses the default browser. Its hover card and overview body are the shared preview's web card. */
export const webKind: KindDef = { kind: 'web', label: 'Web', icon: 'web', get backed() { return nativeWebAvailable(); }, pane: WebPane, preview: WebPreview };

import { WebPane } from '../../web/WebPane';
import { WebPreview } from '../../web/WebPreview';
import type { KindDef } from './slots';

/** A page in a native child view (src-tauri/src/web.rs); outside the desktop app the pane says so and offers the browser. */
export const webKind: KindDef = { kind: 'web', label: 'Web', icon: 'web', backed: true, pane: WebPane, preview: WebPreview };

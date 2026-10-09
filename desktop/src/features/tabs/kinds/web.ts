import { placeholderPane } from './Placeholder';
import { WebPreview } from '../preview/bodies';
import type { KindDef } from './slots';

/** Placeholder. No browser surface or favicon fetch exists; the tab draws a monogram (see KindIcon) and a 2px load line (LoadingLine). */
export const webKind: KindDef = { kind: 'web', label: 'Web', icon: 'web', backed: false, pane: placeholderPane('Web', 'web'), preview: WebPreview };

import { placeholderPane } from './Placeholder';
import { FilePreview } from '../preview/bodies';
import type { KindDef } from './slots';

/** Placeholder. No engine file-read bridge backs a file viewer yet; the file lane fills `pane` and `preview`. */
export const fileKind: KindDef = { kind: 'file', label: 'File', icon: 'fileCode', backed: false, pane: placeholderPane('File', 'fileCode'), preview: FilePreview };

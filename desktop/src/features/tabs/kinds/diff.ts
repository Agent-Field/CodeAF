import { DiffPreview } from '../preview/bodies';
import { FilePane } from '../../files/FilePane';
import type { KindDef } from './slots';

/** Live. The same surface as a file tab, opened on Changes first. */
export const diffKind: KindDef = { kind: 'diff', label: 'Diff', icon: 'diff', backed: true, pane: FilePane, preview: DiffPreview };

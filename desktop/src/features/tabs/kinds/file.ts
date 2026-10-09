import { FilePane } from '../../files/FilePane';
import type { KindDef } from './slots';

/** Live. Reads the file through the engine (File.Text, Diff.Changes, Diff.File), so it works for a remote engine too. */
export const fileKind: KindDef = { kind: 'file', label: 'File', icon: 'fileCode', backed: true, pane: FilePane, preview: null };

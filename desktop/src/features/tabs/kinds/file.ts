import { FilePreview } from '../preview/bodies';
import { FilePane } from '../../files/FilePane';
import { fileIcon, filePaneIcon } from '../../files/fileIcon';
import type { KindDef } from './slots';

/**
 * Live. Reads the file through the engine (File.Text, Diff.Changes, Diff.File), so it works for a remote engine too.
 * The glyph is the file's type (Shell 3j): iconFor reads the path, and glyph reads a title for a specimen that has no pane.
 * A diff tab is a different kind and keeps the diff icon (the design's file-diff).
 */
export const fileKind: KindDef = { kind: 'file', label: 'File', icon: 'fileCode', glyph: fileIcon, iconFor: filePaneIcon, backed: true, pane: FilePane, preview: FilePreview };

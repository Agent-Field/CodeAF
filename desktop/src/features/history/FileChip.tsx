import { Chip, Icon } from '../../components/ui';
import type { HistoryFile } from './types';

const fileName = (path: string) => path.split('/').pop() || path;

/**
 * One changed file as the design draws it: the file glyph, the name and, when the engine counted lines,
 * "+12" in green and "−3" in red. A file with no counts shows only its name (unknown renders as nothing).
 */
export function FileChip({ file }: { file: HistoryFile }) {
  const counted = file.added > 0 || file.removed > 0;
  return <Chip className="history-file" title={file.path}>
    <Icon name="fileCode" size="micro"/>
    <span className="history-file-name">{fileName(file.path)}</span>
    {counted && <><span className="history-file-added">+{file.added}</span><span className="history-file-removed">−{file.removed}</span></>}
  </Chip>;
}

/** A row of file chips, at most `limit` (the rest say how many more). */
export function FileChips({ files, limit = 3 }: { files: readonly HistoryFile[]; limit?: number }) {
  const shown = files.slice(0, limit);
  const more = files.length - shown.length;
  return <div className="history-files">{shown.map(file => <FileChip key={file.path} file={file}/>)}{more > 0 && <span className="history-files-more">+{more} more</span>}</div>;
}

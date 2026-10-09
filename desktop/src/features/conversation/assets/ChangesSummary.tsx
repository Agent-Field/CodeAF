import { useId, useState, type ReactNode } from 'react';
import { Button, Icon } from '../../../components/ui';
import type { FileRef } from '../types';
import { FileChip } from './FileChip';
import './changes.css';

export type ChangesSummaryProps = {
  files: FileRef[];
  /** The work lane's DiffView draws per-file diffs; this component only gives it a place. */
  renderDiff?: (path: string) => ReactNode;
};

/** One row per path: edits to the same file in a turn are one change. */
function merge(files: FileRef[]): FileRef[] {
  const byPath = new Map<string, FileRef>();
  for (const file of files) {
    const seen = byPath.get(file.path);
    byPath.set(file.path, seen ? { ...seen, added: (seen.added ?? 0) + (file.added ?? 0), removed: (seen.removed ?? 0) + (file.removed ?? 0), capped: seen.capped || file.capped } : file);
  }
  return [...byPath.values()];
}

function Totals({ added, removed }: { added: number; removed: number }) {
  return (
    <>
      {added > 0 && <span className="changes-added">+{added}</span>}
      {removed > 0 && <span className="changes-removed">−{removed}</span>}
    </>
  );
}

export function ChangesSummary({ files, renderDiff }: ChangesSummaryProps) {
  const [open, setOpen] = useState(false);
  const listId = useId();
  const rows = merge(files);
  if (rows.length === 0) return null;
  const added = rows.reduce((sum, file) => sum + (file.added ?? 0), 0);
  const removed = rows.reduce((sum, file) => sum + (file.removed ?? 0), 0);
  return (
    <div className="changes" data-open={open}>
      <Button className="changes-head" aria-expanded={open} aria-controls={listId} onClick={() => setOpen(value => !value)}>
        <span className="changes-title">Changed {rows.length} {rows.length === 1 ? 'file' : 'files'}</span>
        <Totals added={added} removed={removed} />
        <Icon name="chevronRight" size="xs" />
      </Button>
      {open ? (
        <ul className="changes-list" id={listId}>
          {rows.map(file => (
            <li key={file.path} className="changes-item">
              <FileChip path={file.path} source={file.source} added={file.added} removed={file.removed} capped={file.capped} />
              {renderDiff?.(file.path)}
            </li>
          ))}
        </ul>
      ) : (
        <span className="changes-chips" id={listId}>
          {rows.map(file => <FileChip key={file.path} path={file.path} source={file.source} added={file.added} removed={file.removed} capped={file.capped} />)}
        </span>
      )}
    </div>
  );
}

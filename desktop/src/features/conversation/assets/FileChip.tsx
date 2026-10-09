import { useState } from 'react';
import { Button, ContextMenu, Icon, type IconName } from '../../../components/ui';
import type { EnginePathFact } from '../../chat/engine-client';
import type { FileRef } from '../types';
import { useAssets, usePathFact } from './AssetContext';
import { fileMenu, type FileAvailability } from './FileActions';
import { fileKind, middleTruncate, relativePath, splitPath, type FileKind } from './paths';
import { PreviewSheet } from './PreviewSheet';
import './FileChip.css';

const dirBudget = 28;

const kindIcon: Record<FileKind, IconName> = {
  image: 'image',
  code: 'fileCode',
  pdf: 'pdf',
  audio: 'audio',
  video: 'video',
  folder: 'folder',
  doc: 'file',
};

export type FileChipProps = {
  path: string;
  /** A fact the caller already holds; without it the chip asks the engine (batched, cached). */
  stat?: EnginePathFact;
  source: FileRef['source'];
  added?: number;
  removed?: number;
  /** Counts were capped by the engine. */
  capped?: boolean;
};

function availabilityOf(path: string, workspace: string, fact?: EnginePathFact): FileAvailability {
  if (fact && !fact.exists) return 'missing';
  if (fact?.outside || (workspace && relativePath(path, workspace) === null)) return 'outside';
  return 'exists';
}

const availabilityIcon: Partial<Record<FileAvailability, IconName>> = { missing: 'fileMissing', outside: 'fileLock' };

const stateNote: Record<FileAvailability, string> = { exists: '', missing: 'not found', outside: 'outside this workspace' };

function Stat({ added, removed, capped }: Pick<FileChipProps, 'added' | 'removed' | 'capped'>) {
  if (!added && !removed) return null;
  const more = capped ? '+' : '';
  return (
    <span className="file-chip-stat">
      {added ? <span className="file-chip-added">+{added}{more}</span> : null}
      {removed ? <span className="file-chip-removed">−{removed}{more}</span> : null}
    </span>
  );
}

export function FileChip({ path, stat, source, added, removed, capped }: FileChipProps) {
  const assets = useAssets();
  const [open, setOpen] = useState(false);
  const looked = usePathFact(path, !!stat);
  const fact = stat ?? looked.fact;
  const { name, dir } = splitPath(path);
  if (!assets.available) return <span className="asset-plain">{name}</span>;
  const availability = availabilityOf(path, assets.workspace, fact);
  const kind = fileKind(name, fact?.dir);
  const icon = availabilityIcon[availability] ?? kindIcon[kind];
  const note = stateNote[availability];
  const canOpen = availability === 'exists' && !fact?.dir;
  return (
    <>
      <ContextMenu label={`Actions for ${name}`} items={fileMenu(path, assets, availability)}>
        <Button
          className="file-chip"
          data-state={availability}
          data-source={source}
          title={[path, note].filter(Boolean).join(' · ')}
          aria-label={[name, note].filter(Boolean).join(', ')}
          aria-disabled={!canOpen || undefined}
          onClick={() => canOpen && setOpen(true)}
        >
          <Icon name={icon} size="xs" />
          <span className="file-chip-name">{name}</span>
          {note ? <span className="file-chip-dir">{note}</span> : dir && <span className="file-chip-dir">{middleTruncate(dir, dirBudget)}</span>}
          <Stat added={added} removed={removed} capped={capped} />
        </Button>
      </ContextMenu>
      {open && <PreviewSheet path={path} onClose={() => setOpen(false)} />}
    </>
  );
}

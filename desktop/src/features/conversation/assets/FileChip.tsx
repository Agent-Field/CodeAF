import { useState, type MouseEvent } from 'react';
import { ChipButton, ContextMenu, Icon, type IconName } from '../../../components/ui';
import type { EnginePathFact } from '../../chat/engine-client';
import type { FileRef } from '../types';
import { useAssets, usePathFact } from './AssetContext';
import { fileMenu, type FileAvailability } from './FileActions';
import { isMac } from '../../../design/keyboard';
import { useOpenFile } from '../../files/OpenFile';
import { displayDir, fileKind, middleTruncate, previewKind, relativePath, splitPath, type FileKind } from './paths';
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
  const openFile = useOpenFile();
  const looked = usePathFact(path, !!stat);
  const fact = stat ?? looked.fact;
  const { name, dir: fullDir } = splitPath(path);
  const dir = displayDir(fullDir, assets.workspace);
  if (!assets.available) return <span className="asset-plain">{name}</span>;
  const availability = availabilityOf(path, assets.workspace, fact);
  const kind = fileKind(name, fact?.dir);
  const icon = availabilityIcon[availability] ?? kindIcon[kind];
  const note = stateNote[availability];
  const canOpen = availability === 'exists' && !fact?.dir;
  // Click opens the preview sheet. Command-click (Control off the Mac) or a middle click opens a text or code file in a tab:
  // its changes first when the turn edited it. Images, PDFs and the rest have no tab to open and keep the sheet.
  const relative = relativePath(path, assets.workspace);
  const openInTab = () => {
    if (!openFile || !relative || previewKind(name) !== 'text') return false;
    openFile(relative, source === 'edit' || source === 'write' || added || removed ? 'diff' : 'file');
    return true;
  };
  const openChip = (event: MouseEvent) => {
    const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
    if (!(primary && openInTab())) setOpen(true);
  };
  return (
    <>
      <ContextMenu label={`Actions for ${name}`} items={fileMenu(path, assets, availability)}>
        <ChipButton
          className="file-chip"
          data-state={availability}
          data-source={source}
          title={[path, note].filter(Boolean).join(' · ')}
          aria-label={[name, note].filter(Boolean).join(', ')}
          aria-disabled={!canOpen || undefined}
          onClick={event => canOpen && openChip(event)}
          onAuxClick={event => { if (canOpen && event.button === 1 && openInTab()) event.preventDefault(); }}
        >
          <Icon name={icon} size="xs" />
          <span className="file-chip-name">{name}</span>
          {note ? <span className="file-chip-dir">{note}</span> : dir && <span className="file-chip-dir">{middleTruncate(dir, dirBudget)}</span>}
          <Stat added={added} removed={removed} capped={capped} />
        </ChipButton>
      </ContextMenu>
      {open && <PreviewSheet path={path} onClose={() => setOpen(false)} />}
    </>
  );
}

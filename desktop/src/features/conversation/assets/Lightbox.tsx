import { useEffect, useState } from 'react';
import { CopyButton, IconButton, Text } from '../../../components/ui';
import { useAssets, useFile } from './AssetContext';
import { revealLabel } from './FileActions';
import { FileChip } from './FileChip';
import { Overlay } from './Overlay';
import { absolutePath, splitPath } from './paths';

export type LightboxItem = { path: string; caption?: string };

type LightboxProps = { items: LightboxItem[]; index: number; onIndex: (index: number) => void; onClose: () => void };

function Stage({ item, actual }: { item: LightboxItem; actual: boolean }) {
  const file = useFile(item.path);
  if (file.status === 'error') return <FileChip path={item.path} stat={{ path: item.path, exists: false, dir: false, size: 0 }} source="image" />;
  if (!file.url) return <Text className="lightbox-note">Loading…</Text>;
  return <img className="lightbox-image" data-actual={actual} src={file.url} alt={item.caption || splitPath(item.path).name} />;
}

/** Full-window viewer for one image or a group: fit or actual size, next/prev, copy, reveal, open. */
export function Lightbox({ items, index, onIndex, onClose }: LightboxProps) {
  const assets = useAssets();
  const [actual, setActual] = useState(false);
  const item = items[index];
  const full = absolutePath(item.path, assets.workspace);
  const step = (by: number) => onIndex((index + by + items.length) % items.length);
  useEffect(() => setActual(false), [index]);
  return (
    <Overlay
      label={`Image ${index + 1} of ${items.length}`}
      variant="lightbox"
      onClose={onClose}
      onKeyDown={event => {
        if (items.length < 2) return;
        if (event.key === 'ArrowRight') step(1);
        if (event.key === 'ArrowLeft') step(-1);
      }}
    >
      <div className="lightbox">
        <header className="lightbox-bar">
          <span className="lightbox-caption">{item.caption || splitPath(item.path).name}</span>
          <span className="lightbox-actions">
            <IconButton label={actual ? 'Fit to window' : 'Actual size'} icon={actual ? 'zoomOut' : 'zoomIn'} iconSize="sm" onClick={() => setActual(value => !value)} />
            <CopyButton text={full} label="Copy path" />
            <IconButton label={revealLabel} icon="folderOpen" iconSize="sm" onClick={() => void assets.revealPath(full).catch(() => undefined)} />
            <IconButton label="Open" icon="external" iconSize="sm" onClick={() => void assets.openPath(full).catch(() => undefined)} />
            <IconButton label="Close" icon="close" iconSize="sm" onClick={onClose} />
          </span>
        </header>
        <div className="lightbox-stage" data-actual={actual}>
          <Stage item={item} actual={actual} />
        </div>
        {items.length > 1 && (
          <>
            <IconButton className="lightbox-prev" label="Previous image" icon="chevronLeft" onClick={() => step(-1)} />
            <IconButton className="lightbox-next" label="Next image" icon="chevronRight" onClick={() => step(1)} />
            <span className="lightbox-count">{index + 1} / {items.length}</span>
          </>
        )}
      </div>
    </Overlay>
  );
}

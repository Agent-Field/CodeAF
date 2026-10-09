import type { ReactNode } from 'react';
import { Text } from '../../../components/ui';
import type { Deliverable } from '../types';
import { useFile } from './AssetContext';
import { ChangesSummary } from './ChangesSummary';
import { FileChip } from './FileChip';
import { ImageFigure } from './ImageFigure';
import { splitPath } from './paths';

type MediaDeliverable = Extract<Deliverable, { kind: 'media' }>;

/** Audio is a slim player row; video shows its first frame with play. Native elements keep it accessible. */
function MediaPlayer({ item }: { item: MediaDeliverable }) {
  const file = useFile(item.path);
  const name = splitPath(item.path).name;
  const label = item.caption || name;
  const player = (): ReactNode => {
    if (file.status === 'error') return null;
    if (!file.url) return <Text className="media-loading">Loading…</Text>;
    return item.media === 'audio'
      ? <audio className="media-audio" controls preload="metadata" src={file.url} aria-label={label} />
      : <video className="media-video" controls preload="metadata" src={file.url} aria-label={label} />;
  };
  return (
    <div className="media" data-media={item.media}>
      {player()}
      <span className="media-meta">
        {item.caption && <span className="media-caption">{item.caption}</span>}
        <FileChip path={item.path} source="task" stat={file.status === 'error' ? { path: item.path, exists: false, dir: false, size: 0 } : undefined} />
      </span>
    </div>
  );
}

export type DeliverableViewProps = { deliverable: Deliverable; renderDiff?: (path: string) => ReactNode };

export function DeliverableView({ deliverable, renderDiff }: DeliverableViewProps) {
  switch (deliverable.kind) {
    case 'image':
      return <ImageFigure path={deliverable.path} caption={deliverable.caption} meta={deliverable.meta} />;
    case 'changes':
      return <ChangesSummary files={deliverable.files} renderDiff={renderDiff} />;
    case 'media':
      return <MediaPlayer item={deliverable} />;
  }
}

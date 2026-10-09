import { useEffect, useRef, useState } from 'react';
import { Button } from '../../../components/ui';
import { FileChip } from './FileChip';
import { useFile } from './AssetContext';
import { Lightbox } from './Lightbox';
import { aspectOf } from './paths';
import './figures.css';

export type FigureItem = { path: string; caption: string; meta: string };

const missing = (path: string) => ({ path, exists: false, dir: false, size: 0 });

/** A neutral box at the right ratio while the bytes travel; a failed read becomes a missing file chip. */
export function ImageThumb({ item, onOpen, square = false }: { item: FigureItem; onOpen: () => void; square?: boolean }) {
  const file = useFile(item.path);
  const frame = useRef<HTMLSpanElement>(null);
  const [ratio, setRatio] = useState(() => aspectOf(item.meta));
  useEffect(() => {
    if (!square && frame.current && ratio) frame.current.style.aspectRatio = String(ratio);
  }, [ratio, square]);
  if (file.status === 'error') return <FileChip path={item.path} stat={missing(item.path)} source="image" />;
  return (
    <span ref={frame} className="figure-box">
      <Button className="figure-frame" data-loading={file.status === 'loading'} aria-label={`Open image: ${item.caption || item.path}`} onClick={onOpen}>
        {file.url && <img src={file.url} alt={item.caption} onLoad={event => setRatio(event.currentTarget.naturalWidth / event.currentTarget.naturalHeight)} />}
      </Button>
    </span>
  );
}

function Caption({ item }: { item: FigureItem }) {
  if (!item.caption && !item.meta) return null;
  return (
    <span className="figure-caption">
      {item.caption}
      {item.meta && <span className="figure-meta">{item.caption ? ' · ' : ''}{item.meta}</span>}
    </span>
  );
}

/** One generated or referenced image in conversation rhythm; click opens the lightbox. */
export function ImageFigure({ path, caption, meta }: FigureItem) {
  const [open, setOpen] = useState(false);
  const item = { path, caption, meta };
  return (
    <span className="image-figure" role="group" aria-label={caption || path}>
      <ImageThumb item={item} onOpen={() => setOpen(true)} />
      <Caption item={item} />
      {open && <Lightbox items={[item]} index={0} onIndex={() => undefined} onClose={() => setOpen(false)} />}
    </span>
  );
}

/** Two or more images as a 2-up grid sharing one lightbox, so next/prev walks the group. */
export function ImageGrid({ items }: { items: FigureItem[] }) {
  const [open, setOpen] = useState<number | null>(null);
  if (items.length === 0) return null;
  return (
    <span className="image-grid" role="group" aria-label={`${items.length} images`}>
      {items.map((item, index) => (
        <span className="image-figure" key={item.path}>
          <ImageThumb item={item} square onOpen={() => setOpen(index)} />
          <Caption item={item} />
        </span>
      ))}
      {open !== null && <Lightbox items={items} index={open} onIndex={setOpen} onClose={() => setOpen(null)} />}
    </span>
  );
}

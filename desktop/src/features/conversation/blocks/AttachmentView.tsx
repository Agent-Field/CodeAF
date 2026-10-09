import { useState } from 'react';
import { FileChip } from '../assets/FileChip';
import { ImageThumb } from '../assets/ImageFigure';
import { Lightbox } from '../assets/Lightbox';
import type { FileRef } from '../types';
import { isPicture } from './attachments';

/** A picture the person attached: a square thumbnail that opens the lightbox. */
function PictureThumb({ path }: { path: string }) {
  const [open, setOpen] = useState(false);
  const item = { path, caption: '', meta: '' };
  return (
    <>
      <ImageThumb item={item} square onOpen={() => setOpen(true)} />
      {open && <Lightbox items={[item]} index={0} onIndex={() => undefined} onClose={() => setOpen(false)} />}
    </>
  );
}

/** Draws one attachment inside the person's bubble: pictures as thumbnails, everything else as a file chip. */
export function renderAttachment(file: FileRef) {
  return isPicture(file) ? <PictureThumb path={file.path} /> : <FileChip path={file.path} source="attachment" />;
}

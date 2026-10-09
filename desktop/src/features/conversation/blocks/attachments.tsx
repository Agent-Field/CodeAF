import type { ReactNode } from 'react';
import type { FileRef } from '../types';

export type RenderAttachment = (file: FileRef) => ReactNode;

const pictureExtension = /\.(png|jpe?g|gif|webp|avif|bmp|svg|heic)$/i;

export function isPicture(file: FileRef): boolean {
  return pictureExtension.test(file.path);
}

/** Pictures become a thumbnail grid, files become chips; the renderer draws each. */
export function Attachments({ files, render }: { files: FileRef[]; render: RenderAttachment }) {
  if (files.length === 0) return null;
  const pictures = files.filter(isPicture);
  const others = files.filter((file) => !isPicture(file));
  return (
    <div className="attachments">
      {pictures.length > 0 && (
        <div className="attachments-pictures">
          {pictures.map((file) => (
            <div key={file.path} className="attachments-thumb">
              {render(file)}
            </div>
          ))}
        </div>
      )}
      {others.length > 0 && (
        <div className="attachments-files">
          {others.map((file) => (
            <span key={file.path}>{render(file)}</span>
          ))}
        </div>
      )}
    </div>
  );
}

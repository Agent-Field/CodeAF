import { useEffect, useState } from 'react';
import { Text } from '../../components/ui';
import type { EngineFile } from '../chat/engine-client';
import type { Load } from './useWorkView';
import './image-view.css';

type Props = {
  image?: Load<EngineFile>;
  state?: { line: string | null; url?: string };
  name: string;
  deleted: boolean;
  onBroken: (url: string) => void;
};

/** Blob ownership follows the engine answer, so replacing a picture or closing its pane releases the bytes. */
export function ImageView({ image, state, name, deleted, onBroken }: Props) {
  const file = image?.status === 'ready' ? image.value : undefined;
  const source = state?.line ? undefined : state?.url;
  const [blob, setBlob] = useState<{ source: string; url: string }>();
  useEffect(() => {
    if (!source || !file) return;
    const bytes = Uint8Array.from(atob(file.dataBase64), char => char.charCodeAt(0));
    const mime = file.mime.split(';')[0].trim().toLowerCase();
    const url = URL.createObjectURL(new Blob([bytes], { type: mime }));
    setBlob({ source, url });
    return () => URL.revokeObjectURL(url);
  }, [source, file]);
  const line = deleted && image?.status === 'failed' ? 'This file was deleted.' : state?.line;
  if (line) return <Text className="file-message">{line}</Text>;
  if (!source || blob?.source !== source) return <Text className="file-message">Loading…</Text>;
  // SVG stays inside the browser's restricted image document, never in the renderer DOM.
  return <img className="file-picture" src={blob.url} alt={name} onError={() => onBroken(source)}/>;
}

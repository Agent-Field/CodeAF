import { useCallback, useEffect, useRef, useState } from 'react';
import { admit, isPicture, type Attachment } from './attachments';

let counter = 0;

function make(file: File): Attachment {
  const picture = isPicture(file);
  const previewUrl = picture ? URL.createObjectURL(file) : undefined;
  return { id: `attachment-${counter++}`, file, picture, previewUrl };
}

function revoke(item: Attachment) {
  if (item.previewUrl) URL.revokeObjectURL(item.previewUrl);
}

export function useAttachments() {
  const [items, setItems] = useState<Attachment[]>([]);
  const [error, setError] = useState<string>();
  const live = useRef(items);
  live.current = items;

  const add = useCallback((files: File[]) => {
    if (files.length === 0) return;
    const { accepted, error: refused } = admit(live.current, files);
    setError(refused);
    if (accepted.length) setItems(current => [...current, ...accepted.map(make)]);
  }, []);

  const remove = useCallback((id: string) => {
    const gone = live.current.find(item => item.id === id);
    if (gone) revoke(gone);
    setItems(current => current.filter(item => item.id !== id));
    setError(undefined);
  }, []);

  const clear = useCallback(() => {
    live.current.forEach(revoke);
    setItems([]);
    setError(undefined);
  }, []);

  useEffect(() => () => live.current.forEach(revoke), []);

  return { items, error, add, remove, clear };
}

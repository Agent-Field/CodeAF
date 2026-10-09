// Messages sent with Queue, listed above the composer until the engine records
// them as the person's own message. The engine has no way to take one back, so
// removing a row only hides it here.

import { useEffect, useMemo, useState } from 'react';
import type { EngineEntry } from '../chat/engine-client';
import type { QueuedItem } from './blocks/QueuedRows';

type Queued = QueuedItem & { after: number; hidden: boolean };

let counter = 0;

function recorded(item: Queued, entries: EngineEntry[]): boolean {
  return entries.slice(item.after).some((entry) => entry.Role === 'user' && !entry.Steer && entry.Text === item.text);
}

export function useQueued(entries: EngineEntry[]) {
  const [items, setItems] = useState<Queued[]>([]);
  const [removedHere, setRemovedHere] = useState(false);
  const pending = useMemo(() => items.filter((item) => !recorded(item, entries)), [items, entries]);

  useEffect(() => {
    if (pending.length !== items.length) setItems(pending);
    if (pending.length === 0) setRemovedHere(false);
  }, [pending, items.length]);

  function add(text: string) {
    setItems((before) => [...before, { id: `queued-${counter++}`, text, after: entries.length, hidden: false }]);
  }

  function remove(id: string) {
    setItems((before) => before.map((item) => (item.id === id ? { ...item, hidden: true } : item)));
    setRemovedHere(true);
  }

  const shown = pending.filter((item) => !item.hidden).map(({ id, text }) => ({ id, text }));
  return { items: shown, add, remove, removedHere };
}

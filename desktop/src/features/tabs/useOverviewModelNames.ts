import { useEffect, useState } from 'react';
import { MODELS_CHANGED, readModelCatalog, readPinnedModels } from '../chat/engine-client';
import { orderModels } from '../conversation/composer/modelOrder';

/** One read per open overview, shared by all cards. Reads labels, never changes roles or pins. */
export function useOverviewModelNames(open: boolean): Readonly<Record<string, string>> {
  const [names, setNames] = useState<Record<string, string>>({});
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const refresh = () => setRevision(value => value + 1);
    window.addEventListener(MODELS_CHANGED, refresh);
    return () => window.removeEventListener(MODELS_CHANGED, refresh);
  }, []);
  useEffect(() => {
    if (!open) return;
    let current = true;
    Promise.all([readModelCatalog(), readPinnedModels().catch(() => ({ pinned: [], chosen: false }))]).then(([catalog, pins]) => {
      if (!current) return;
      const { models } = orderModels(catalog.models, pins.pinned, '', model => model.name || model.id);
      setNames(Object.fromEntries(models.map(model => [model.id, model.short || model.label])));
    }).catch(() => { if (current) setNames({}); });
    return () => { current = false; };
  }, [open, revision]);
  return names;
}

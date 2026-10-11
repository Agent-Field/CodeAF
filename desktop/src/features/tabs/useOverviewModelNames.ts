import { useEffect, useState } from 'react';
import { MODELS_CHANGED, readModelCatalog, readPinnedModels } from '../chat/engine-client';
import { orderModels } from '../conversation/composer/modelOrder';
import { useLoaded } from './preview/useLoaded';

/** Model labels are shared and cached until settings change, so reopening adds no metadata reads. */
export function useOverviewModelNames(open: boolean): Readonly<Record<string, string>> {
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const refresh = () => setRevision(value => value + 1);
    window.addEventListener(MODELS_CHANGED, refresh);
    return () => window.removeEventListener(MODELS_CHANGED, refresh);
  }, []);
  const names = useLoaded(open ? 'overview/model-names' : undefined, async () => {
    const [catalog, pins] = await Promise.all([readModelCatalog(), readPinnedModels().catch(() => ({ pinned: [], chosen: false }))]);
    const { models } = orderModels(catalog.models, pins.pinned, '', model => model.name || model.id);
    return Object.fromEntries(models.map(model => [model.id, model.short || model.label]));
  }, String(revision));
  return names ?? {};
}

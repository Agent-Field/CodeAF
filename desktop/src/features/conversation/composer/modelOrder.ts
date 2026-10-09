import type { ModelOption } from './ModelPicker';

/** What the composer needs to know about a catalog model. */
export type CatalogEntry = { id: string; name?: string };
export type PinnedEntry = { id: string; label: string };

/** Models pinned to the segmented control and to ⌘1-3. */
export const PINNED_LIMIT = 3;

/**
 * The picker's list: the person's pinned models first, in their order (those the catalog really offers),
 * then the model in use if it is not pinned, then everything else. The pinned count is how many leading
 * models are segments and take ⌘1-3. When none of the pinned models is on offer the model in use stands alone.
 */
export function orderModels<T extends CatalogEntry>(catalog: readonly T[], pinned: readonly PinnedEntry[], selectedId: string, labelOf: (model: T) => string): { models: ModelOption[]; pinnedCount: number } {
  const byId = new Map(catalog.map(model => [model.id, model]));
  const lead = pinned.filter(entry => byId.has(entry.id)).slice(0, PINNED_LIMIT);
  const segments = lead.length > 0 ? lead : (byId.has(selectedId) ? [{ id: selectedId, label: '' }] : []);
  const shown = new Set(segments.map(entry => entry.id));
  const extra = byId.has(selectedId) && !shown.has(selectedId) ? [selectedId] : [];
  const rest = catalog.filter(model => !shown.has(model.id) && !extra.includes(model.id));
  const option = (model: T, label?: string): ModelOption => ({ id: model.id, label: labelOf(model), short: label || undefined });
  const models = [
    ...segments.map(entry => option(byId.get(entry.id)!, entry.label)),
    ...extra.map(id => option(byId.get(id)!)),
    ...rest.map(model => option(model)),
  ];
  return { models, pinnedCount: segments.length };
}

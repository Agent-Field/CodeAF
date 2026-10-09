import { useCallback, useEffect, useMemo, useState } from 'react';
import { CONVERSATION_ROLE, ENGINE_MODEL, MODELS_CHANGED, readModelCatalog, readModelRoles, readPinnedModels, setModelRole, type CatalogModel, type ModelRole, type PinnedModel } from '../../chat/engine-client';
import { PINNED_LIMIT, type ModelOption } from './ModelPicker';
import { orderModels } from './modelOrder';
import type { EffortControl } from './ModelPopover';

export const DEFAULT_MODEL_LABEL = 'DeepSeek v4.1 Flash';
/** The default model's pinned short label (owner decision D4), shipped so a fresh tab reads it with no engine call. */
export const DEFAULT_MODEL_SHORT = 'DS Flash';

/** The default model reads as its design name; every other model reads as the provider names it. */
const labelOf = (model: CatalogModel): string => (model.id === ENGINE_MODEL ? DEFAULT_MODEL_LABEL : model.name || model.id);
const EFFORT_LABELS: Record<string, string> = { low: 'Low', medium: 'Medium', high: 'High' };

export type ConversationModel = {
  models: ModelOption[]; pinnedCount: number; selectedId: string; onSelect: (id: string) => void; effort?: EffortControl;
  /**
   * Set when something above the person's pick decides the model (a place's default). The chip then names that model and
   * offers no swap, because swapping the role would not change what runs; `by` is the deciding place's name.
   */
  decided?: { by: string };
};

/**
 * The composer picker's model, wired to the engine's "Conversation" role: the list is the provider's,
 * a pick is saved in the profile and moves every open chat, and the next message is sent on it.
 * Returns nothing while the engine cannot say (the chip then shows the model without offering a swap).
 */
export function useConversationModel(current: string | undefined, readBeforeSession = false, decided?: { model: string; by: string }): ConversationModel | undefined {
  const [catalog, setCatalog] = useState<CatalogModel[]>();
  const [role, setRole] = useState<ModelRole>();
  const [pinned, setPinned] = useState<PinnedModel[]>([]);
  const [reads, setReads] = useState(0);
  const [picked, setPicked] = useState<string>();
  // Blank conversation tabs defer reads; Home opts in to the saved role before creating its first session.
  const attached = current !== undefined || readBeforeSession;
  useEffect(() => {
    if (!attached) return;
    let live = true;
    Promise.all([readModelCatalog(), readModelRoles(), readPinnedModels().catch(() => ({ pinned: [] as PinnedModel[], chosen: false }))]).then(([listed, roles, pins]) => {
      if (!live) return;
      setCatalog(listed.models);
      setRole(roles.roles.find(row => row.id === CONVERSATION_ROLE));
      setPinned(pins.pinned);
    }).catch(() => undefined);
    return () => { live = false; };
  }, [attached, reads]);
  // The settings page saves pins and role models; the picker reads them again when it does.
  useEffect(() => {
    const again = () => setReads(count => count + 1);
    window.addEventListener(MODELS_CHANGED, again);
    return () => window.removeEventListener(MODELS_CHANGED, again);
  }, []);
  // The engine's own report of the model replaces the optimistic pick as soon as it arrives.
  useEffect(() => setPicked(undefined), [current]);
  // Before the first send there is no session to ask, so the saved choice of the role stands in.
  const selectedId = decided?.model ?? picked ?? current ?? role?.model ?? '';
  const select = useCallback((model: string, effort = '') => {
    setPicked(model);
    void setModelRole(CONVERSATION_ROLE, { model, effort }).then(setRole).catch(() => setPicked(undefined));
  }, []);
  // The person's pinned models lead, so they are the segments and the first chords.
  const ordered = useMemo(() => catalog ? orderModels(catalog, pinned, selectedId, labelOf) : { models: [] as ModelOption[], pinnedCount: 0 }, [catalog, pinned, selectedId]);
  const { models, pinnedCount } = ordered;
  const efforts = catalog?.find(model => model.id === selectedId)?.efforts?.filter(word => EFFORT_LABELS[word]);
  const effort = useMemo<EffortControl | undefined>(() => (efforts && efforts.length > 0 ? {
    value: role?.model === selectedId ? role.effort ?? '' : '',
    options: efforts.map(word => ({ value: word, label: EFFORT_LABELS[word] })),
    onChange: word => select(selectedId, word),
  } : undefined), [efforts, role, selectedId, select]);
  // A decided model is shown as the engine will run it, whether or not the provider list is readable or includes it.
  if (decided) {
    const known = catalog?.find(model => model.id === decided.model);
    return { models: [{ id: decided.model, label: known ? labelOf(known) : decided.model }], pinnedCount: 0, selectedId: decided.model, onSelect: () => undefined, decided: { by: decided.by } };
  }
  if (!catalog || models.length === 0 || !models.some(model => model.id === selectedId)) return undefined;
  return { models, pinnedCount, selectedId, onSelect: id => select(id), effort };
}

export { PINNED_LIMIT };

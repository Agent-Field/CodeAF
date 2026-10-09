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
/** The default model keeps its pinned short word unless the person pinned it under another. */
const withDefaultShort = (option: ModelOption): ModelOption => (option.id === ENGINE_MODEL && !option.short ? { ...option, short: DEFAULT_MODEL_SHORT } : option);
// Retained panes share one in-flight read of the global catalog/roles/pins, so a focus event does not
// multiply identical requests. This is coalescing only; the next refresh always asks the engine again.
let readingModels: ReturnType<typeof readModels> | undefined;
function readModels() {
  return Promise.allSettled([readModelCatalog(), readModelRoles(), readPinnedModels()] as const);
}
function refreshModels(fresh = false) {
  // Foreground refresh must not reuse a read started before the other window saved its role.
  if (fresh) return readModels();
  if (!readingModels) readingModels = readModels().finally(() => { readingModels = undefined; });
  return readingModels;
}
const EFFORT_LABELS: Record<string, string> = { low: 'Low', medium: 'Medium', high: 'High' };

export type ConversationModel = {
  models: ModelOption[]; pinnedCount: number; selectedId: string; onSelect: (id: string) => void; effort?: EffortControl;
  /**
   * Set when the chip names a model but cannot swap it: a place's default decides it (swapping the role would not change
   * what runs), or the provider's list is unreadable so only the saved role's own model is known.
   */
  readOnly?: boolean;
  /** Why the chip reads as it does, drawn as the chip's tooltip; set when a place decides the model. */
  hint?: string;
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
    refreshModels(reads > 0).then(([listed, roles, pins]) => {
      if (!live) return;
      setCatalog(listed.status === 'fulfilled' ? listed.value.models : undefined);
      setRole(roles.status === 'fulfilled' ? roles.value.roles.find(row => row.id === CONVERSATION_ROLE) : undefined);
      setPinned(pins.status === 'fulfilled' ? pins.value.pinned : []);
    });
    return () => { live = false; };
  }, [attached, reads]);
  // Another window may save the role without this window receiving MODELS_CHANGED. Refresh on actual
  // foreground arrival as well; quiet unattached tabs still make no reads.
  useEffect(() => {
    if (!attached) return;
    const again = () => setReads(count => count + 1);
    const foreground = () => { if (document.visibilityState === 'visible') again(); };
    window.addEventListener(MODELS_CHANGED, again);
    window.addEventListener('focus', foreground);
    document.addEventListener('visibilitychange', foreground);
    return () => {
      window.removeEventListener(MODELS_CHANGED, again);
      window.removeEventListener('focus', foreground);
      document.removeEventListener('visibilitychange', foreground);
    };
  }, [attached]);
  // The engine's own report of the model replaces the optimistic pick as soon as it arrives.
  useEffect(() => setPicked(undefined), [current]);
  // Before the first send there is no session to ask, so the saved choice of the role stands in.
  const selectedId = decided?.model ?? picked ?? current ?? role?.model ?? '';
  const select = useCallback((model: string, effort = '') => {
    setPicked(model);
    void setModelRole(CONVERSATION_ROLE, { model, effort }).then(saved => { setRole(saved); if (current === undefined) setPicked(undefined); }).catch(() => setPicked(undefined));
  }, [current]);
  // The person's pinned models lead, so they are the segments and the first chords.
  const ordered = useMemo(() => catalog ? orderModels(catalog, pinned, selectedId, labelOf) : { models: [] as ModelOption[], pinnedCount: 0 }, [catalog, pinned, selectedId]);
  const { models, pinnedCount } = ordered;
  const efforts = catalog?.find(model => model.id === selectedId)?.efforts?.filter(word => EFFORT_LABELS[word]);
  const effort = useMemo<EffortControl | undefined>(() => (efforts && efforts.length > 0 ? {
    value: role?.model === selectedId ? role.effort ?? '' : '',
    options: efforts.map(word => ({ value: word, label: EFFORT_LABELS[word] })),
    onChange: word => select(selectedId, word),
  } : undefined), [efforts, role, selectedId, select]);
  // A decided model is shown as the engine will run it, whether or not the provider list is readable or includes it. The
  // option is the one the shared ordering builds (pinned short word included); a model the list does not know keeps the
  // id the engine reported, never a guessed name.
  if (decided) {
    const shown = ordered.models.find(model => model.id === decided.model) ?? { id: decided.model, label: decided.model };
    return { models: [withDefaultShort(shown)], pinnedCount: 0, selectedId: decided.model, onSelect: () => undefined, readOnly: true, hint: `Model set by ${decided.by}.` };
  }
  // Before a session exists the saved role is the whole truth. With the list unreadable that is still one model to name,
  // but only once the role itself was read: an unread role names nothing, because a guessed model is a wrong one.
  if (readBeforeSession && role?.model && !models.some(model => model.id === selectedId)) {
    return { models: [withDefaultShort({ id: role.model, label: role.model === ENGINE_MODEL ? DEFAULT_MODEL_LABEL : role.model })], pinnedCount: 0, selectedId: role.model, onSelect: () => undefined, readOnly: true };
  }
  if (!catalog || models.length === 0 || !models.some(model => model.id === selectedId)) return undefined;
  return { models: models.map(withDefaultShort), pinnedCount, selectedId, onSelect: id => select(id), effort };
}

export { PINNED_LIMIT };

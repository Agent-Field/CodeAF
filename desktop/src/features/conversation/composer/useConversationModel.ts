import { useCallback, useEffect, useMemo, useState } from 'react';
import { CONVERSATION_ROLE, ENGINE_MODEL, readModelCatalog, readModelRoles, setModelRole, type CatalogModel, type ModelRole } from '../../chat/engine-client';
import { PINNED_LIMIT, type ModelOption } from './ModelPicker';
import type { EffortControl } from './ModelPopover';

export const DEFAULT_MODEL_LABEL = 'DeepSeek v4.1 Flash';

/** The default model reads as its design name; every other model reads as the provider names it. */
const labelOf = (model: CatalogModel): string => (model.id === ENGINE_MODEL ? DEFAULT_MODEL_LABEL : model.name || model.id);
const EFFORT_LABELS: Record<string, string> = { low: 'Low', medium: 'Medium', high: 'High' };

export type ConversationModel = { models: ModelOption[]; selectedId: string; onSelect: (id: string) => void; effort?: EffortControl };

/**
 * The composer picker's model, wired to the engine's "Conversation" role: the list is the provider's,
 * a pick is saved in the profile and moves every open chat, and the next message is sent on it.
 * Returns nothing while the engine cannot say (the chip then shows the model without offering a swap).
 */
export function useConversationModel(current: string | undefined): ConversationModel | undefined {
  const [catalog, setCatalog] = useState<CatalogModel[]>();
  const [role, setRole] = useState<ModelRole>();
  const [picked, setPicked] = useState<string>();
  // A new tab makes no engine call, so the list and the role are read only once a session exists.
  const attached = current !== undefined;
  useEffect(() => {
    if (!attached) return;
    let live = true;
    Promise.all([readModelCatalog(), readModelRoles()]).then(([listed, roles]) => {
      if (!live) return;
      setCatalog(listed.models);
      setRole(roles.roles.find(row => row.id === CONVERSATION_ROLE));
    }).catch(() => undefined);
    return () => { live = false; };
  }, [attached]);
  // The engine's own report of the model replaces the optimistic pick as soon as it arrives.
  useEffect(() => setPicked(undefined), [current]);
  // Before the first send there is no session to ask, so the saved choice of the role stands in.
  const selectedId = picked ?? current ?? role?.model ?? '';
  const select = useCallback((model: string, effort = '') => {
    setPicked(model);
    void setModelRole(CONVERSATION_ROLE, { model, effort }).then(setRole).catch(() => setPicked(undefined));
  }, []);
  const models = useMemo<ModelOption[]>(() => {
    if (!catalog) return [];
    // The default and the model in use lead, so they are the pinned segments and the first two chords.
    const byId = new Map(catalog.map(model => [model.id, model]));
    const lead = [ENGINE_MODEL, selectedId].filter((id, at, all) => id && all.indexOf(id) === at);
    const ordered = [...lead.map(id => byId.get(id) ?? { id, name: id }), ...catalog.filter(model => !lead.includes(model.id))];
    return ordered.map(model => ({ id: model.id, label: labelOf(model), short: model.id === ENGINE_MODEL ? 'Flash' : undefined }));
  }, [catalog, selectedId]);
  const efforts = catalog?.find(model => model.id === selectedId)?.efforts?.filter(word => EFFORT_LABELS[word]);
  const effort = useMemo<EffortControl | undefined>(() => (efforts && efforts.length > 0 ? {
    value: role?.model === selectedId ? role.effort ?? '' : '',
    options: efforts.map(word => ({ value: word, label: EFFORT_LABELS[word] })),
    onChange: word => select(selectedId, word),
  } : undefined), [efforts, role, selectedId, select]);
  if (!catalog || models.length === 0 || !models.some(model => model.id === selectedId)) return undefined;
  return { models, selectedId, onSelect: id => select(id), effort };
}

export { PINNED_LIMIT };

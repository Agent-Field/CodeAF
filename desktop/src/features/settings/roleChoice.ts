export type RoleChoice = { model: string; effort?: string };

/**
 * The full choice a role save sends. An effort change names no model, so it takes the model of the latest queued
 * choice (else the saved one): otherwise an effort click made while a model save is still in flight would write
 * the old model back over it. A model change names no effort, so the effort is cleared with it, because the new
 * model may not accept it.
 */
export function resolveRoleChoice(patch: { model?: string; effort?: string }, queued: RoleChoice | undefined, saved: string): RoleChoice {
  return patch.model === undefined ? { model: queued?.model ?? saved, effort: patch.effort } : { model: patch.model, effort: patch.effort };
}

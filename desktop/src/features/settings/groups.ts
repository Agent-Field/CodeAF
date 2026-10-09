import type { ModelRole, ModelRoleCategory } from '../chat/engine-client';

/** The sections the page shows when an older engine sends roles without them: everything in one list. */
const ALL_JOBS: ModelRoleCategory = { id: '', name: 'Jobs' };

export type RoleSection = { category: ModelRoleCategory; roles: ModelRole[] };

/**
 * The roles under their sections, in the engine's section order and the engine's role order within each. A role
 * whose section the engine did not list is kept, in a last "Other jobs" section, rather than dropped from the page.
 */
export function groupRoles(roles: readonly ModelRole[], categories: readonly ModelRoleCategory[] | undefined): RoleSection[] {
  if (!categories?.length) return roles.length ? [{ category: ALL_JOBS, roles: [...roles] }] : [];
  const known = new Set(categories.map(category => category.id));
  const sections = categories.map(category => ({ category, roles: roles.filter(role => role.category === category.id) }));
  const other = roles.filter(role => !known.has(role.category ?? ''));
  if (other.length) sections.push({ category: { id: 'other', name: 'Other jobs' }, roles: other });
  return sections.filter(section => section.roles.length > 0);
}

/** The line under a role that is not doing anything yet, or following another role; empty otherwise. */
export function roleStateLine(role: ModelRole, roles: readonly ModelRole[]): string {
  if (role.live === false) return 'Not in use yet. Your choice is kept for when it is.';
  if (role.inherits && !role.chosen) {
    const parent = roles.find(row => row.id === role.inherits);
    if (parent) return `Follows ${parent.name} until you choose.`;
  }
  return '';
}

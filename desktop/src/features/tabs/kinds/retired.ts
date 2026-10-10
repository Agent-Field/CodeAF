/** Inbox is removed before kind fallback so an old navigation slot cannot become a conversation. */
export const isRetiredInbox = (value: unknown): boolean => !!value && typeof value === 'object' && 'kind' in value && value.kind === 'inbox';

/** Preserve the surviving contents of an old split, including when only one real pane remains. */
export function withoutInbox(values: unknown[]): unknown[] {
  return values.flatMap(value => {
    if (isRetiredInbox(value)) return [];
    if (!value || typeof value !== 'object' || !('split' in value)) return [value];
    const split = value.split;
    if (!split || typeof split !== 'object' || !('panes' in split) || !Array.isArray(split.panes) || !split.panes.some(isRetiredInbox)) return [value];
    const panes = split.panes.filter(pane => !isRetiredInbox(pane));
    if (!panes.length) return [];
    if (panes.length === 1) return [{ ...value, ...panes[0], id: 'id' in value ? value.id : undefined, split: undefined }];
    const focus = 'focus' in split && typeof split.focus === 'number' ? split.focus : 0;
    const focused = split.panes[focus];
    return [{ ...value, split: { ...split, panes, focus: Math.max(0, panes.indexOf(focused)) } }];
  });
}

// The one door from the shell (rail, chooser, Home, native windows) into the window's tab strip. The workspace owns
// its tabs; anything else asks it to change them by sending one typed action here, as the rail already asks for the
// Settings tab (shell/shellState.ts). Nothing is queued: an action sent while no strip is mounted is dropped, and the
// sender learns that from the boolean.
import type { WorkspaceAction } from '../../tabs/model';

export const workspaceActionEvent = 'codeaf:workspace-action';

/** Returns true when a mounted strip took the action. */
export function requestWorkspace(action: WorkspaceAction): boolean {
  const event = new CustomEvent<WorkspaceAction>(workspaceActionEvent, { detail: action, cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

export function onWorkspaceRequest(handler: (action: WorkspaceAction) => void): () => void {
  const listener = (event: Event) => { event.preventDefault(); handler((event as CustomEvent<WorkspaceAction>).detail); };
  window.addEventListener(workspaceActionEvent, listener);
  return () => window.removeEventListener(workspaceActionEvent, listener);
}

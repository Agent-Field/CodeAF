import { expect, type Page } from '@playwright/test';
import type { WorkspaceState } from '../../../src/features/tabs/model';

/** Reads the real per-window saved copy, including offline queued intent, rather than the frozen v1 import.
 * The DOM projection is the convergence condition: no sleeps and no test-only application state door. */
export async function savedWorkspace(page: Page): Promise<WorkspaceState> {
 let result: WorkspaceState | undefined;
 await expect.poll(async () => {
  const sample = await page.evaluate(async () => {
   const controllerPath = '/src/features/workspace-sync/controller.ts';
   const modelPath = '/src/features/tabs/model.ts';
   const sharedPath = '/src/features/workspace-sync/shared.ts';
   const { createWorkspaceController } = await import(controllerPath);
   const { readWorkspace } = await import(modelPath);
   const { sharedOf } = await import(sharedPath);
   const { windowWriter } = await import('/src/features/workspace-sync/windowStore.ts');
   const writer = windowWriter();
   const raw = writer && localStorage.getItem(`codeaf.desktop.workspace-sync.v1:now:${writer}`);
   if (!raw) return undefined;
   const saved = JSON.parse(raw);
   const persisted = { ...saved, base: saved.base ?? sharedOf(readWorkspace()) };
   const state = createWorkspaceController({ key: 'now', writer, initial: readWorkspace, persisted, client: {} }).getState();
   const panes = state.tabs.flatMap((tab: { split?: { panes: {id:string;title:string}[] };id:string;title:string }) => tab.split?.panes ?? [tab]);
   const actual = [...document.querySelectorAll('.workspace-tabstrip [role="tab"]')].map(node => [node.id, node.getAttribute('aria-label'), node.closest('.workspace-tab-group')?.querySelector('.workspace-group-name')?.textContent ?? null, node.closest('.workspace-tab')?.classList.contains('is-pinned') ?? false]);
   const wanted = panes.map((pane: {id:string;title:string}) => { const owner = state.tabs.find((tab: {id:string;split?:{panes:{id:string}[]}}) => tab.id === pane.id || tab.split?.panes.some(p => p.id === pane.id)); return [`tab-${pane.id}`, pane.title, state.groups.find((group: {id:string;title:string}) => group.id === owner?.groupId)?.title ?? null, !!owner?.pinned]; });
   const groups = [...document.querySelectorAll('.workspace-group-name')].map(node => node.textContent);
   return { state, matches: JSON.stringify(actual) === JSON.stringify(wanted) && JSON.stringify(groups) === JSON.stringify(state.groups.map((group: {title:string}) => group.title)) };
  });
  if (sample?.matches) result = sample.state;
  return !!sample?.matches;
 }, { message: 'window-saved tab intent converges with the actual strip' }).toBe(true);
 return result!;
}

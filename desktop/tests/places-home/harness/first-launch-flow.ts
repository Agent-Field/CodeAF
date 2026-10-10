import { createFromFolder } from '../../../src/features/places/shell/homeActions';
import { createPlacesClient } from '../../../src/features/places/client';
import type { PlacesShell } from '../../../src/features/places/shell/PlacesShell';

/** The browser cannot open an OS chooser, so this fixture supplies only its typed result and keeps the real Places transport. */
export async function firstLaunchFlow(status: 'picked' | 'cancelled' | 'busy') {
  const navigated: string[] = [];
  const client = createPlacesClient(async (path, request) => {
    const response = await fetch(`/api/engine${path}`, { method: request.method,
      headers: { 'Content-Type': 'application/json' }, body: request.method === 'GET' ? undefined : JSON.stringify(request.body) });
    if (!response.ok) throw new Error('Folder could not be opened.');
    return response.json();
  });
  const shell = { client, native: { pickFolder: async () => ({ status, paths: [{ name: 'nested', path: '/work/repo/nested' }] }) },
    write: async (_text: string, job: () => Promise<unknown>) => [await job()], goTo: async (id: string) => { navigated.push(id); },
  } as unknown as PlacesShell;
  try { await createFromFolder(shell); return { navigated }; }
  catch (error) { return { navigated, error: (error as Error).message }; }
}

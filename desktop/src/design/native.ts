import { invoke, isTauri } from '@tauri-apps/api/core';

const DESKTOP_ONLY = 'Opening files needs the desktop app';

export async function openPath(path: string, workspace: string): Promise<void> {
  if (!isTauri()) throw new Error(DESKTOP_ONLY);
  await invoke('open_path', { path, workspace });
}

export async function revealPath(path: string, workspace: string): Promise<void> {
  if (!isTauri()) throw new Error(DESKTOP_ONLY);
  await invoke('reveal_path', { path, workspace });
}

export async function openUrl(url: string): Promise<void> {
  if (isTauri()) {
    await invoke('open_url', { url });
    return;
  }
  window.open(url, '_blank', 'noopener,noreferrer');
}

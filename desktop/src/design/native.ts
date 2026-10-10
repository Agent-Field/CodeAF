import { invoke, isTauri } from '@tauri-apps/api/core';

const DESKTOP_ONLY = 'Opening files needs the desktop app';

// The window does not name the directory. Rust asks the engine which roots are
// open and refuses anything outside them. `workspace` is accepted for one wave
// so existing callers still compile, and it is ignored.
export function openPath(path: string): Promise<void>;
export function openPath(path: string, workspace: string): Promise<void>;
export async function openPath(path: string, workspace?: string): Promise<void> {
  void workspace;
  if (!isTauri()) throw new Error(DESKTOP_ONLY);
  await invoke('open_path', { path });
}

export function revealPath(path: string): Promise<void>;
export function revealPath(path: string, workspace: string): Promise<void>;
export async function revealPath(path: string, workspace?: string): Promise<void> {
  void workspace;
  if (!isTauri()) throw new Error(DESKTOP_ONLY);
  await invoke('reveal_path', { path });
}

/** This machine's name, or undefined in a browser (where nothing local can be opened anyway). */
export async function hostName(): Promise<string | undefined> {
  if (!isTauri()) return undefined;
  try {
    return await invoke<string>('host_name');
  } catch {
    return undefined;
  }
}

export async function openUrl(url: string): Promise<void> {
  if (isTauri()) {
    await invoke('open_url', { url });
    return;
  }
  window.open(url, '_blank', 'noopener,noreferrer');
}

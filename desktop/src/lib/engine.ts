import { invoke, isTauri } from '@tauri-apps/api/core';
export interface EngineHealth { status: string; version: string; platform: string }
export async function checkEngine(): Promise<EngineHealth> {
 if (!isTauri()) throw new Error('Open the desktop app to check the local Go engine.');
 return invoke<EngineHealth>('engine_health');
}

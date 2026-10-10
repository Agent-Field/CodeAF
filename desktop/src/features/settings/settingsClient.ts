import { EngineError, engineJson } from '../chat/engine-client.ts';

/** Source names are a closed vocabulary so this field cannot carry a provider key. */
export type KeySource = 'OPENROUTER_API_KEY' | 'OPENAI_API_KEY' | 'profile';
export type KeyStatus = { present: boolean; source?: KeySource };
export type EngineFacts = { local: boolean; connection: 'local' | 'forwarded'; model?: string; version?: string };
/** The engine supplies its allowed modes; this client does not duplicate its registry. */
export type Permissions = { mode: string; modes: string[] };

const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const invalid = (what: string): never => { throw new EngineError(`The engine returned invalid ${what}.`); };
const optionalString = (value: unknown): value is string | undefined => value === undefined || typeof value === 'string';

/** Only presence and a recognized source leave this boundary, even if a server sends extra fields. */
export async function keyStatus(): Promise<KeyStatus> {
 const value = await engineJson<unknown>('/settings/key');
 if (!object(value) || typeof value.present !== 'boolean') return invalid('key status');
 if (value.source !== undefined && !['OPENROUTER_API_KEY', 'OPENAI_API_KEY', 'profile'].includes(value.source as string)) return invalid('key status');
 if (!value.present && value.source !== undefined) return invalid('key status');
 return value.source === undefined ? { present: value.present } : { present: value.present, source: value.source as KeySource };
}

/** Missing model and version stay absent rather than being replaced with client defaults. */
export async function engineFacts(): Promise<EngineFacts> {
 const value = await engineJson<unknown>('/settings/engine');
 if (!object(value) || typeof value.local !== 'boolean' || !['local', 'forwarded'].includes(value.connection as string)
  || value.local !== (value.connection === 'local') || !optionalString(value.model) || !optionalString(value.version)) return invalid('engine facts');
 return { local: value.local, connection: value.connection as EngineFacts['connection'],
  ...(value.model === undefined ? {} : { model: value.model }), ...(value.version === undefined ? {} : { version: value.version }) };
}

function permissionStatus(value: unknown): Permissions {
 if (!object(value) || typeof value.mode !== 'string' || !value.mode || !Array.isArray(value.modes)
  || !value.modes.every(mode => typeof mode === 'string' && !!mode) || !value.modes.includes(value.mode)) return invalid('permissions');
 return { mode: value.mode, modes: value.modes.slice() };
}

export async function permissions(): Promise<Permissions> {
 return permissionStatus(await engineJson<unknown>('/settings/permissions'));
}

/** The returned persisted mode is authoritative; a rejected write never invents a successful change. */
export async function setPermissions(mode: string): Promise<Permissions> {
 return permissionStatus(await engineJson<unknown>('/settings/permissions', { method: 'PUT', body: JSON.stringify({ mode }) }));
}

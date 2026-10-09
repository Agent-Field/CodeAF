import { readEngineFile } from '../../chat/engine-client';

/** Full command output lives in a file the engine wrote; read it through the engine's file door. */
export async function readEngineText(sessionId: string, path: string): Promise<string> {
  const file = await readEngineFile(sessionId, path);
  const bytes = Uint8Array.from(atob(file.dataBase64), (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

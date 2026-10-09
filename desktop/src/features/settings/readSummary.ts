import { CONVERSATION_ROLE, readModelRoles, readPinnedModels } from '../chat/engine-client';
import { settingsSummary } from './summary';

/** The overview card's text for the Settings tab, read from the engine; empty while the engine cannot say. */
export async function readSettingsSummary(): Promise<string> {
  try {
    const [pins, roles] = await Promise.all([readPinnedModels(), readModelRoles()]);
    return settingsSummary({ pinned: pins.pinned, conversationEffort: roles.roles.find(role => role.id === CONVERSATION_ROLE)?.effort });
  } catch {
    return '';
  }
}

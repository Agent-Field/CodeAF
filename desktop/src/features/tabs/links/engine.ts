// The engine reads a link is resolved through. Each one is a read of something the engine already keeps; none of them
// starts a conversation, a turn or a terminal. Attaching a saved conversation (to read a task or a terminal under it)
// is the same attach a saved tab makes when it is shown.
import { readTaskPage, readTerminal } from '../../chat/engine-client';
import { archiveHistory, readHistory } from '../../history/client';
import { chatIdFromSessionFile } from '../../places/client';
import { sessionFor } from '../../terminal/open';
import type { LinkEngine } from './openLink';

export const engineLinks: LinkEngine = {
  async chat(chatId) {
    const { item } = await readHistory(chatId);
    // The engine names the transcript; it must be the conversation the link named, or the link opens nothing.
    if (!item || typeof item.sessionFile !== 'string' || chatIdFromSessionFile(item.sessionFile) !== chatId) throw new Error('The engine answered for a different conversation.');
    return { sessionFile: item.sessionFile, title: typeof item.title === 'string' ? item.title : '', archived: item.archived === true };
  },
  async unarchive(chatId) { await archiveHistory([chatId], false); },
  async task(sessionFile, taskId) {
    const session = await sessionFor(sessionFile);
    const page = await readTaskPage(session.id, taskId);
    return { title: typeof page.Row?.Title === 'string' ? page.Row.Title : '' };
  },
  async terminal(sessionFile, terminalId) {
    const session = await sessionFor(sessionFile);
    const info = await readTerminal(session.id, terminalId);
    return { title: info.title };
  },
};

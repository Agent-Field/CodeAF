import { useRef, useState, type KeyboardEvent } from 'react';
import { isMac } from '../../../design/keyboard';
import { connectEngine, sendEngine, sendEngineWithFiles, type EngineSnapshot, type OutgoingFile } from '../../chat/engine-client';
import { Composer } from '../../conversation/Composer';
import { DEFAULT_MODEL_LABEL, DEFAULT_MODEL_SHORT } from '../../conversation/composer/useConversationModel';
import { newTab } from '../../tabs/helpers';
import type { WorkspaceAction } from '../../tabs/model';
import { chatIdFromSessionFile } from '../client';
import type { PlacesShell } from './PlacesShell';

type HomeComposerProps = {
  shell: PlacesShell;
  placeId: string;
  placeName: string;
  draft: string;
  onDraft: (draft: string) => void;
  /** The strip the new chat opens in. Without one the composer is not drawn. */
  dispatch: (action: WorkspaceAction) => void;
  /** Read-only while the engine is unreachable: the words stay, nothing is sent. */
  offline?: boolean;
};

const titleOf = (text: string) => text.split('\n').find(line => line.trim())?.trim().slice(0, 80) || 'New conversation';

/**
 * The Home composer (Places 8a, 9b): the one place a chat starts in this place. ↵ opens it as a new tab right after
 * Home and focuses it; ⌘↵ (Ctrl ↵) opens it in the background so several can be fired off in a row. Home never turns
 * into a chat.
 *
 * The order is the canonical one: the first send creates the session (POST /sessions), the new chat is filed in this
 * place, and only then does its first turn go out, so the engine reads the place's context from the very first turn.
 * If filing is refused nothing is sent, the words stay, and a second send reuses the session already made instead of
 * leaving another empty one behind.
 */
export function HomeComposer({ shell, placeId, placeName, draft, onDraft, dispatch, offline }: HomeComposerProps) {
  const background = useRef(false);
  const created = useRef<{ snapshot: EngineSnapshot; filed: boolean }>(undefined);
  const [error, setError] = useState<string>();

  async function onSend(text: string, _mode: string, files?: OutgoingFile[]): Promise<boolean> {
    const behind = background.current;
    background.current = false;
    setError(undefined);
    try {
      const session = created.current ?? { snapshot: await connectEngine(undefined, placeId), filed: false };
      created.current = session;
      if (!session.filed) {
        const chatId = chatIdFromSessionFile(session.snapshot.sessionFile);
        if (!chatId) throw new Error('The engine did not say where the new chat is saved, so it was not filed. Nothing was sent.');
        try { await shell.client.addChats(placeId, [chatId], { addedBy: 'you' }); }
        catch (failure) { throw new Error(`The chat could not be filed in “${placeName}”: ${failure instanceof Error ? failure.message : 'the engine refused'}. Nothing was sent; your words are kept.`); }
        session.filed = true;
        void shell.refresh();
      }
      const id = session.snapshot.id;
      if (files?.length) await sendEngineWithFiles(id, text, files); else await sendEngine(id, text);
      created.current = undefined;
      const tab = newTab({ kind: 'conversation', title: titleOf(text), titleSource: 'message', sessionFile: session.snapshot.sessionFile });
      dispatch({ type: 'open', tab, background: behind, at: 1 });
      return true;
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : 'The chat did not start. Your words are kept.');
      return false;
    }
  }

  // The Composer sends on ↵ whatever the modifiers; the primary modifier held with it is what makes this a background start.
  const noteModifier = (event: KeyboardEvent) => { if (event.key === 'Enter') background.current = isMac ? event.metaKey : event.ctrlKey; };

  return <div className="home-composer-field" onKeyDownCapture={noteModifier}>
    {error && <p className="home-composer-error" role="alert">{error}</p>}
    <Composer draft={draft} onDraft={onDraft} onSend={onSend} onStop={() => undefined} running={false} docked
      disabledReason={offline ? 'Reconnecting to the engine…' : undefined}
      placeholder={`Start something in ${placeName}`} modelLabel={DEFAULT_MODEL_LABEL} modelShort={DEFAULT_MODEL_SHORT} autoFocus={false}/>
  </div>;
}

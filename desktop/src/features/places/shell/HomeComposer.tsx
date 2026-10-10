import { useRef, useState, type KeyboardEvent } from 'react';
import { isMac } from '../../../design/keyboard';
import { connectEngine, sendEngine, sendEngineWithFiles, type EngineSnapshot, type OutgoingFile } from '../../chat/engine-client';
import { Composer } from '../../conversation/Composer';
import { useConversationModel } from '../../conversation/composer/useConversationModel';
import { newTab } from '../../tabs/helpers';
import type { WorkspaceAction } from '../../tabs/model';
import { chatIdFromSessionFile } from '../client';
import { homeComposerPlaceholder } from './homeComposerCopy';
import type { PlacesShell } from './PlacesShell';
import { useEffectiveModel } from './useEffectiveModel';

type HomeComposerProps = {
  shell: PlacesShell;
  placeId: string;
  placeName: string;
  draft: string;
  onDraft: (draft: string) => void;
  /** The strip the new chat opens in. Without one the composer is not drawn. */
  dispatch: (action: WorkspaceAction) => void;
  /** Places 8b: this Home is the empty screen, so the prompt asks for the first chat. */
  empty?: boolean;
  /** Read-only while the engine is unreachable: the words stay, nothing is sent. */
  offline?: boolean;
};

const titleOf = (text: string) => text.split('\n').find(line => line.trim())?.trim().slice(0, 80) || 'New conversation';

/**
 * The Home composer (Places 8a, 8b, 9b): the one place a chat starts in this place. ↵ opens it as a new tab right
 * after Home and focuses it; ⌘↵ (Ctrl ↵) opens it in the background so several can be fired off in a row. Home never
 * turns into a chat.
 *
 * The order is the canonical one: the first send creates the session (POST /sessions {placeId}). The bridge files
 * the chat in that place and opens it in the place's first usable folder; the client confirms the filing before the
 * first turn, so a mock that does not file inside the open still cannot send an unfiled chat. If filing is refused
 * nothing is sent, the words stay, and a second send reuses the session already made instead of leaving another
 * empty one behind.
 */
export function HomeComposer({ shell, placeId, placeName, draft, onDraft, dispatch, empty, offline }: HomeComposerProps) {
  // The chip names the model the first message will really run on: the places' decision when they make one, else the
  // saved Conversation role. While that is unknown it names nothing, because a guessed model is a wrong one.
  const effective = useEffectiveModel(shell.client, placeId, shell.places.graph?.revision, offline);
  const say = effective.kind === 'known' ? effective.say : undefined;
  const decided = say?.state === 'applies' && say.model ? { model: say.model, by: say.decidedBy?.name ?? 'a place' } : undefined;
  const model = useConversationModel(undefined, true, decided);
  const settled = effective.kind === 'known';
  // When places disagree nothing is applied; the chip's tooltip says so rather than a paragraph above the composer.
  const disagree = say?.state === 'needsPick' && say.wanted?.length ? `${say.wanted.map(place => place.name).join(' and ')} choose different models, so you pick one in the chat.` : undefined;
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

  // The Composer sends on ↵ without Shift or Alt. Only that keystroke may arm a background start; a newline or a
  // composed Enter must not leave the flag set for the next real send.
  const noteModifier = (event: KeyboardEvent) => {
    if (event.key !== 'Enter') return;
    const sends = !event.shiftKey && !event.altKey && !event.nativeEvent.isComposing && event.nativeEvent.keyCode !== 229;
    background.current = sends && (isMac ? event.metaKey : event.ctrlKey);
  };

  return <div className="home-composer-field" onKeyDownCapture={noteModifier}>
    {error && <p className="home-composer-error" role="alert">{error}</p>}
    <Composer variant="home" draft={draft} onDraft={onDraft} onSend={onSend} onStop={() => undefined} running={false} docked
      disabledReason={offline ? 'Reconnecting to the engine…' : undefined}
      placeholder={homeComposerPlaceholder(placeName, !!empty)} model={settled && model ? { ...model, hint: model.hint ?? disagree } : undefined} autoFocus={false}/>
  </div>;
}

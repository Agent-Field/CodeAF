import { createContext } from 'react';

/**
 * What must happen between a NEW conversation's canonical creation (the first send's POST /sessions) and its first
 * turn. The Places shell provides it in a place's strip: it files the new chat in that place, so the engine reads the
 * place's context at the very first turn (Places 9e "a chat started there belongs to the current place too"). It is
 * awaited; if it rejects, nothing is sent and the person's words stay in the field with Retry, because a chat that ran
 * its first turn outside the place it was started in would have answered without that place's context.
 * Absent (Now, a bare window) means nothing happens and the turn goes out as it always did.
 */
export type BeforeFirstTurn = (sessionFile: string) => Promise<void>;
export const FirstTurnContext = createContext<BeforeFirstTurn | undefined>(undefined);

/** The place must be supplied when the host is created, before its working folder is chosen. Saved attachments ignore it. */
export const NewConversationPlaceContext = createContext<string | undefined>(undefined);

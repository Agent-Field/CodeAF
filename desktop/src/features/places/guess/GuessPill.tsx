import { useEffect, useMemo, useRef, useState } from 'react';
import { Select } from '../../../components/ui';
import type { EngineSnapshot } from '../../chat/engine-client';
import { chatIdFromSessionFile, placesTransport } from '../client';
import { usePlacesShell } from '../shell/PlacesShell';
import { createGuessController } from './controller';
import { canFileChoice, filingOptions } from './filing';
import type { Guess } from './model';
import './guess.css';

export type GuessPillProps = { guess?: Guess; choices: readonly { id: string; name: string }[]; selected?: string; onSelect: (id: string) => void; disabled?: boolean };
/** The shared select supplies keyboard focus, dismissal and the same themed menu as the composer model chip. */
export function GuessPill({ guess, choices, selected, onSelect, disabled }: GuessPillProps) {
  if (!guess) return null;
  const options = choices.map(place => ({ value: place.id, label: `→ ${place.name}` }));
  if (!options.some(option => option.value === guess.place)) options.unshift({ value: guess.place, label: `→ ${guess.name}` });
  return <Select className="place-guess-pill" label="Change suggested place" value={selected ?? guess.place} options={options} onValueChange={onSelect} disabled={disabled}/>;
}

/** A choice stays local until the first settled answer. Normal automatic filing remains the engine's own decision. */
export function ComposerGuess({ draft, snapshot, disabled }: { draft: string; snapshot?: EngineSnapshot; disabled?: boolean }) {
  const shell = usePlacesShell();
  const [answer, setAnswer] = useState<{ draft: string; guess?: Guess }>();
  const latestDraft = useRef(draft);
  latestDraft.current = draft;
  const guess = answer?.draft === draft ? answer.guess : undefined;
  const [selected, setSelected] = useState<string>();
  const committed = useRef(false);
  const controller = useMemo(() => createGuessController((ask, signal) => placesTransport('/places/guess', { method: 'POST', body: ask, signal }), guess => setAnswer({ draft: latestDraft.current, guess })), []);
  const chatId = snapshot ? chatIdFromSessionFile(snapshot.sessionFile) : '';
  const settled = canFileChoice(snapshot);
  useEffect(() => {
    if (shell && !disabled && !settled) controller.update({ draft, workspace: snapshot?.workspace ?? '', chatId });
    else controller.update({ draft: '', workspace: '', chatId: '' });
    return () => controller.dispose();
  }, [controller, draft, snapshot?.workspace, chatId, settled, disabled, !!shell]);
  useEffect(() => {
    if (!shell || !settled || !selected || !chatId || committed.current) return;
    committed.current = true;
    void shell.write('Changed the chat’s place', async () => {
      const current = await shell.client.chatPlaces(chatId);
      return shell.client.addChats(selected, [chatId], filingOptions(current.places, selected));
    })
      .catch(failure => { committed.current = false; shell.warn(failure); });
  }, [settled, selected, chatId, shell]);
  return <GuessPill guess={guess} selected={selected} onSelect={setSelected} choices={shell?.places.graph?.places.filter(place => !place.archived) ?? []} disabled={disabled}/>;
}

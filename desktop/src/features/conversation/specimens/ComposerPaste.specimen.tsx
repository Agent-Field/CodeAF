import { useEffect, useRef, useState } from 'react';
import { Composer } from '../Composer';
import { encodePasted } from '../composer/pastedText';
import { UserMessage } from '../UserMessage';

const LOG = Array.from({ length: 214 }, (_, i) =>
  i === 0 ? '--- FAIL: TestLoad/env_override (0.01s)' : `    load_test.go:${100 + i}: parse: unexpected ']' at 1:1`,
).join('\n');

const LONG_TYPED =
  "We load configs from three places: the repo root, the user's home dir, and an env override. " +
  'All three go through the same parser, but the env path is read first and cached, so a bad file there ' +
  'poisons every later load.\n\n1. Cover the repo root.\n2. Cover the home dir.\n3. Cover the env override.\n' +
  '4. Check the cache.\n5. Check the error position.\n6. Check trailing commas.\n7. Check nested tables.\n8. Done.';

/** The paste arrives as a real paste event, so the specimen exercises the real handler. */
function PastedCase() {
  const host = useRef<HTMLDivElement>(null);
  const [draft, setDraft] = useState('Why does nested fail but flat pass?');
  const pasted = useRef(false);
  useEffect(() => {
    if (pasted.current) return;
    pasted.current = true;
    const clipboardData = new DataTransfer();
    clipboardData.setData('text/plain', LOG);
    host.current?.querySelector('textarea')?.dispatchEvent(new ClipboardEvent('paste', { clipboardData, bubbles: true }));
  }, []);
  return (
    <section ref={host}>
      <p>Pasted text: a card above the field, with remove</p>
      <Composer draft={draft} onDraft={setDraft} onSend={() => true} onStop={() => undefined} running={false} docked />
    </section>
  );
}

function TypedCase() {
  const [draft, setDraft] = useState(LONG_TYPED);
  return (
    <section>
      <p>Typed text caps at 8 lines, then scrolls under a top fade</p>
      <Composer draft={draft} onDraft={setDraft} onSend={() => true} onStop={() => undefined} running={false} docked />
    </section>
  );
}

export function ComposerPasteSpecimen() {
  return (
    <div>
      <PastedCase />
      <TypedCase />
      <section>
        <p>Sent: the pasted card in the user bubble</p>
        <UserMessage text={encodePasted([LOG], "This morning's CI log. Why does nested fail but flat pass?")} />
      </section>
    </div>
  );
}

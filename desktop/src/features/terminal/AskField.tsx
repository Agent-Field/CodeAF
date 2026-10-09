import { useState, type FormEvent } from 'react';
import { IconButton, Text, TextInput } from '../../components/ui';

/** Sends the question with the output attached; rejects with the sentence to show when it cannot. */
export type AskSubmit = (question: string) => Promise<void>;

/**
 * Design 3c: the 40px pill under the output. It starts a new conversation with the output attached (Q5),
 * so the answer to Enter is a new tab, never a reply inside this one. A refusal reads as one muted line above.
 */
export function AskField({ onAsk }: { onAsk: AskSubmit }) {
  const [question, setQuestion] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true); setError('');
    try { await onAsk(question); setQuestion(''); }
    catch (failure) { setError(failure instanceof Error ? failure.message : 'The question was not sent.'); }
    finally { setBusy(false); }
  }
  return <div className="terminal-ask">
    {error && <Text className="terminal-ask-note" role="alert">{error}</Text>}
    <form className="terminal-ask-field" onSubmit={submit}>
      <TextInput className="terminal-ask-input" aria-label="Ask codeaf about this output" placeholder="Ask codeaf about this output" value={question} onChange={event => setQuestion(event.target.value)} autoComplete="off" spellCheck={false}/>
      <IconButton className="terminal-ask-send" type="submit" label="Ask" icon="send" iconSize="sm" disabled={busy} data-ready={question.trim() !== '' || undefined}/>
    </form>
  </div>;
}

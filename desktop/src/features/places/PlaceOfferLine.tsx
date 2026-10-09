import { useRef, useState } from 'react';
import { Button, Icon } from '../../components/ui';
import { proposalsClient, type PlaceProposal } from './proposals-client';
import { usePlacesShell } from './shell/PlacesShell';
import './home.css';

/** The same quiet suggestion line in All places and after the first reply; canonical writes use the shell's receipt Undo. */
export function PlaceOfferLine({ proposal, text, action, onSettled }: { proposal: PlaceProposal; text: string; action: string; onSettled: () => void }) {
  const shell = usePlacesShell();
  const held = useRef(false);
  const [busy, setBusy] = useState(false);
  const [decided, setDecided] = useState(false);
  if (!shell || decided) return null;
  async function decide(accept: boolean) {
    if (!shell || held.current) return;
    held.current = true; setBusy(true);
    try {
      if (accept) await shell.write(text, () => proposalsClient.accept(proposal));
      else await proposalsClient.decline(proposal.id);
      setDecided(true);
    } catch (failure) { shell.warn(failure); }
    finally { held.current = false; setBusy(false); onSettled(); }
  }
  return <div className="home-suggestion" aria-label="Place suggestion">
    <Icon name="sparkles" size="micro"/><span>{text}</span><span aria-hidden="true">·</span>
    <Button variant="ghost" className="home-suggestion-action" disabled={busy} onClick={() => void decide(true)}>{action}</Button>
    <Button variant="ghost" className="home-suggestion-action" disabled={busy} onClick={() => void decide(false)}>Not now</Button>
  </div>;
}

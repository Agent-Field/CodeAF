// TEST-ONLY page. It mounts the real header chip beside the real conversation bar, over an invented (mock) list, and logs each
// callback. It is not part of the production build, which only builds the root index.html.
import { useState } from 'react';
import { ConversationBar } from '../../../src/features/conversation/ConversationBar';
import { UsingLine, usingVisible } from '../../../src/features/places/UsingLine';
import { useUsing } from '../../../src/features/places/useUsing';
import { createUsingClient } from '../../../src/features/places/using-client';
import type { PolicyField, SourceHandoff } from '../../../src/features/places/using-types';
import { mockApi, type Calls, type Scenario } from './mock';
import './harness.css';

declare global { interface Window { __usingCalls: Calls; __heal?: () => void } }

// One mock bridge per page load, made outside the component so StrictMode's double render cannot hand the page and the test two different ones.
const params = new URLSearchParams(location.search);
const scenario = (params.get('scenario') ?? 'full') as Scenario;
const calls: Calls = (window.__usingCalls = []);
const bridge = mockApi(scenario, calls, { choiceFails: params.get('choiceFails') === '1' });
window.__heal = (bridge as unknown as { heal?: () => void } | undefined)?.heal;
// The page drives the REAL typed client (and so its answer validation) over the in-memory bridge: a mock shape the client would refuse fails here.
const api = bridge && createUsingClient(async (path, request) => {
  const [, , token, , tail] = path.split('/');
  const body = request.body as { field: PolicyField; placeId: string };
  if (request.method === 'GET') return bridge.using(decodeURIComponent(token), request.signal);
  return tail === 'choice' ? bridge.choose(decodeURIComponent(token), body.field, body.placeId) : bridge.apply(decodeURIComponent(token), body.field);
});

export function UsingSpecimen() {
  const control = useUsing(api, 'mock-token');
  const [log, setLog] = useState<string[]>([]);
  const say = (line: string) => setLog(previous => [...previous.slice(-7), line]);
  const wired = params.get('open') !== '0';
  const onOpenSource = (handoff: SourceHandoff) => say(`open:${JSON.stringify(handoff)}`);
  return (
    <div className="using-harness" data-testid="using-specimen">
      <p className="using-harness-mock">Mock fixture. These places, sources and settings are invented for the tests and are not a real conversation.</p>
      <div className="using-harness-pane">
        {(usingVisible(control) || params.get('title') !== '0') && (
          <ConversationBar lead="title" counts={{ running: 0, needsYou: 0 }} using={<UsingLine control={control} onOpenSource={wired ? onOpenSource : undefined} onAddToPlace={params.get('add') === '1' ? () => say('add-to-place') : undefined} startOpen={params.get('open-sheet') === '1'} />}>
            <span className="conversation-bar-title">Release notes for v2.4.1</span>
          </ConversationBar>
        )}
        <p className="using-harness-body">The conversation continues here.</p>
      </div>
      <ul aria-label="Callback log" className="using-harness-log">{log.map((line, index) => <li key={index}>{line}</li>)}</ul>
    </div>
  );
}

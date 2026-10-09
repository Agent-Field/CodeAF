import { useState } from 'react';
import { Composer } from '../Composer';

function Case(props: { title: string; initial: string; running?: boolean; reason?: string }) {
  const [draft, setDraft] = useState(props.initial);
  return (
    <section>
      <p>{props.title}</p>
      <Composer
        draft={draft}
        onDraft={setDraft}
        onSend={() => true}
        onStop={() => undefined}
        running={props.running ?? false}
        docked
        disabledReason={props.reason}
        modelLabel="DeepSeek v4.1 Flash"
        onAttach={() => undefined}
        tasksToggle={{ label: 'Tasks · 3/5', onClick: () => undefined }}
        recallLast={() => 'Count the words in README.md'}
      />
    </section>
  );
}

export function ComposerSpecimen() {
  return (
    <div>
      <Case title="Idle, empty" initial="" />
      <Case title="Typing" initial="Summarise the repo layout and list the entry points" />
      <Case title="Running, empty (Stop)" initial="" running />
      <Case title="Running, with text (Steer)" initial="Also skip the vendor folder" running />
      <Case title="Disabled" initial="" reason="Message the main conversation to change this task" />
    </div>
  );
}

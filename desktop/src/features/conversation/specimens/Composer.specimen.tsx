import { useState } from 'react';
import { Composer } from '../Composer';
import { ModelPicker } from '../composer/ModelPicker';
import { ComposerAttachmentsSpecimen } from './ComposerAttachments.specimen';
import { ComposerPasteSpecimen } from './ComposerPaste.specimen';


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
        tasksToggle={{ label: 'Tasks · 3/5', onClick: () => undefined }}
        recallLast={() => 'Count the words in README.md'}
      />
    </section>
  );
}

const SPECIMEN_MODELS = [
  { id: 'flash', label: 'DeepSeek v4.1 Flash' },
  { id: 'pro', label: 'DeepSeek v4.1 Pro' },
  { id: 'sonnet', label: 'Claude Sonnet' },
];
const SPECIMEN_EFFORT = [{ value: 'low', label: 'Low' }, { value: 'medium', label: 'Medium' }, { value: 'high', label: 'High' }];

/** The model popover with the routing and effort the live engine does not expose yet (design 1e). */
function ModelPickerCase() {
  const [model, setModel] = useState('flash');
  const [effort, setEffort] = useState('medium');
  return (
    <section aria-label="Model popover try-out">
      <p>Model popover, specimen routing and effort</p>
      <ModelPicker models={SPECIMEN_MODELS} selectedId={model} onSelect={setModel} effort={{ value: effort, options: SPECIMEN_EFFORT, onChange: setEffort }} />
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
      <ModelPickerCase />
      <ComposerAttachmentsSpecimen />
      <ComposerPasteSpecimen />
    </div>
  );
}

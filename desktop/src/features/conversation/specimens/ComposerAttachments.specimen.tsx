import { useEffect, useRef, useState, type RefObject } from 'react';
import { sendEngineWithFiles, type OutgoingFile } from '../../chat/engine-client';
import { Composer } from '../Composer';

const SHADES = ['#c9b79c', '#8aa1a8'];

async function picture(name: string, shade: string) {
  const canvas = document.createElement('canvas');
  canvas.width = 96;
  canvas.height = 96;
  const context = canvas.getContext('2d')!;
  context.fillStyle = shade;
  context.fillRect(0, 0, 96, 96);
  const blob = await new Promise<Blob>(resolve => canvas.toBlob(value => resolve(value!), 'image/png'));
  return new File([blob], name, { type: 'image/png' });
}

const report = new File(['quarterly numbers'], 'quarterly-report.csv', { type: 'text/csv' });
const tooBig = () => new File([new ArrayBuffer(11 * 1024 * 1024)], 'site-photo.png', { type: 'image/png' });

function transfer(files: File[]) {
  const data = new DataTransfer();
  files.forEach(file => data.items.add(file));
  return data;
}

type Stage = 'pictures' | 'error' | 'page' | 'over';

/** Fixtures arrive as real paste events, so the specimen exercises the real handlers. */
function useStaged(stage: Stage, host: RefObject<HTMLDivElement | null>) {
  useEffect(() => {
    let live = true;
    void (async () => {
      const files = [await picture('sketch.png', SHADES[0]), await picture('screenshot.png', SHADES[1]), report];
      const root = host.current;
      if (!live || !root) return;
      const pasted = stage === 'error' ? [...files, tooBig()] : files;
      if (stage === 'pictures' || stage === 'error') {
        const clipboardData = transfer(pasted);
        root.querySelector('textarea')!.dispatchEvent(new ClipboardEvent('paste', { clipboardData, bubbles: true }));
      }
    })();
    return () => {
      live = false;
    };
  }, [stage, host]);
}

function Case({ title, stage }: { title: string; stage: Stage }) {
  const dropState = stage === 'page' || stage === 'over' ? stage : undefined;
  const host = useRef<HTMLDivElement>(null);
  const [draft, setDraft] = useState('Compare these two against the report');
  useStaged(stage, host);
  return (
    <section ref={host}>
      <p>{title}</p>
      <Composer draft={draft} onDraft={setDraft} onSend={() => true} onStop={() => undefined} running={false} docked dropState={dropState} />
    </section>
  );
}

/** Sends to a mock session so a test can read the request; harmlessly refused anywhere else. */
function TryIt() {
  const [draft, setDraft] = useState('');
  const [sent, setSent] = useState(0);
  const send = async (text: string, _mode: string, files: OutgoingFile[] = []) => {
    try {
      await sendEngineWithFiles('specimen', text, files);
      setSent(count => count + 1);
      return true;
    } catch {
      return false;
    }
  };
  return (
    <section aria-label="Attachment try-out">
      <p>Try it{sent ? ` · sent ${sent}` : ''}</p>
      <Composer draft={draft} onDraft={setDraft} onSend={send} onStop={() => undefined} running={false} docked />
    </section>
  );
}

export function ComposerAttachmentsSpecimen() {
  return (
    <div>
      <Case title="Two pictures and a file" stage="pictures" />
      <Case title="Too large: named in a quiet line" stage="error" />
      <Case title="Dragging over the page" stage="page" />
      <Case title="Dragging over the composer" stage="over" />
      <TryIt />
    </div>
  );
}

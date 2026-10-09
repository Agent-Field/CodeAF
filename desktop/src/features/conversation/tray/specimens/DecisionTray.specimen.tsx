import { useEffect, useState } from 'react';
import { Text } from '../../../../components/ui';
import type { EngineAnswer, EngineQuestion } from '../../../chat/engine-client';
import { DecisionTray } from '../DecisionTray';
import { ReceiptLine } from '../ReceiptLine';
import { fixtures } from './fixtures';

function useNow() {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

/** One tray with its fixture questions; the last answer sent is echoed as the engine would read it. */
function Case({ title, questions }: { title: string; questions: EngineQuestion[] }) {
  const now = useNow();
  const [sent, setSent] = useState<EngineAnswer[]>([]);
  const answer = (value: EngineAnswer) => {
    setSent((before) => [...before, { ...value }]);
    return Promise.resolve(true);
  };
  return (
    <div className="tray-specimen">
      <Text tone="default">{title}</Text>
      <DecisionTray
        questions={questions}
        busyKey={null}
        now={now}
        onAnswer={answer}
        onHold={() => undefined}
        renderImage={(block) => <img src={block.path} alt={block.title ?? ''} />}
      />
      {sent.length > 0 && <Text className="tray-caption">{`Sent: ${JSON.stringify(sent[sent.length - 1])}`}</Text>}
    </div>
  );
}

export function DecisionTraySpecimen() {
  const cases = fixtures(Date.now());
  return (
    <div className="tray-specimen-stack">
      {cases.map(({ title, questions }) => <Case key={title} title={title} questions={questions} />)}
      <div className="tray-specimen">
        <Text tone="default">Receipt lines</Text>
        <ReceiptLine state="waiting" text="Waiting on you: `rm -rf build`" onFocus={() => undefined} />
        <ReceiptLine state="answered" text="Allowed once · you · 14:02" />
        <ReceiptLine state="answered" text="Keep strict · picked by codeaf after 30s" />
        <ReceiptLine state="withdrawn" text="No longer needed. The turn moved on." />
      </div>
    </div>
  );
}

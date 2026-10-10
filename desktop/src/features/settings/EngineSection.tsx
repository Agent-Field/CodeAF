import { useEffect, useRef, useState } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { Button, SectionHeading } from '../../components/ui';
import { devEngineAddress, ENGINE_CONNECTED, ENGINE_RECONNECTING, ENGINE_UNREACHABLE, engineWhere, readEngineWhere } from './enginePlace';

type Phase = 'reconnecting' | 'connected' | 'unreachable';

const WORD: Record<Phase, string> = {
  reconnecting: ENGINE_RECONNECTING,
  connected: ENGINE_CONNECTED,
  unreachable: ENGINE_UNREACHABLE,
};

/**
 * Engine: where this window's engine runs, and whether it answers. The state is
 * ink-2, never red (I-IFL-6). Retry is the conversation's quiet ink-3 and exists
 * only while the engine is unreachable. A fresh attempt hides it so the line can
 * say it is reconnecting.
 */
export function EngineSection() {
  const [phase, setPhase] = useState<Phase>('reconnecting');
  const [where, setWhere] = useState('');
  const attempt = useRef(0);
  const inflight = useRef<AbortController | null>(null);

  const probe = () => {
    const id = ++attempt.current;
    inflight.current?.abort();
    const caller = new AbortController();
    inflight.current = caller;
    setPhase('reconnecting');
    void readEngineWhere(caller.signal).then(
      reading => {
        if (attempt.current !== id) return;
        // A failed desktop check may not have learned a URL. Keep the last place.
        setWhere(current => reading.where || current);
        setPhase(reading.ok ? 'connected' : 'unreachable');
      },
      () => {
        // A newer attempt has moved the counter, so this one must not draw. A check that ran out of time still does.
        if (attempt.current !== id) return;
        setWhere(current => engineWhere({ desktop: isTauri(), devAddress: devEngineAddress() }) || current);
        setPhase('unreachable');
      },
    );
  };

  useEffect(() => {
    probe();
    return () => {
      attempt.current += 1;
      inflight.current?.abort();
    };
  }, []);

  return (
    <section className="models-section" aria-labelledby="settings-engine">
      <SectionHeading id="settings-engine">Engine</SectionHeading>
      <div className="settings-row">
        {where ? <span className="settings-row-name settings-engine-where">{where}</span> : null}
        <p className="settings-row-state" role="status" data-engine-state={phase}>{WORD[phase]}</p>
        {phase === 'unreachable' && (
          <Button variant="ghost" className="settings-engine-retry" onClick={probe}>Retry</Button>
        )}
      </div>
    </section>
  );
}

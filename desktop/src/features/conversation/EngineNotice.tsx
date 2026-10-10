import { useEffect, useRef, useState } from 'react';
import { Button } from '../../components/ui';
import {
  RECONNECTING_NOTICE,
  UNREACHABLE_NOTICE,
  reconnectAdvance,
  reconnectDown,
  reconnectOnline,
  reconnectRetry,
  reconnectSettled,
  reconnectWake,
  type ReconnectPhase,
} from './offline/reconnect';

type Props = {
  /** True while the engine is not answering. The line is absent otherwise. */
  active: boolean;
  /**
   * One check of the engine, without sending the draft. Resolves true when
   * the engine answered. The machine decides when to call it.
   */
  onProbe: () => Promise<boolean> | boolean;
};

/**
 * Drives the pure machine off the window clock. Playwright's fake clock
 * advances both, so the 30s change does not need a real wait. The probe
 * callback is a ref so a new closure does not restart the window.
 */
function useReconnectNotice(active: boolean, onProbe: Props['onProbe']) {
  const machine = useRef(reconnectOnline());
  const probe = useRef(onProbe);
  probe.current = onProbe;
  const generation = useRef(0);
  const [phase, setPhase] = useState<ReconnectPhase>('online');

  useEffect(() => {
    const gen = ++generation.current;
    if (!active) {
      machine.current = reconnectOnline();
      setPhase('online');
      return () => { generation.current += 1; };
    }
    // A second run of this effect (strict mode) keeps the outage already started.
    if (machine.current.phase === 'online') {
      machine.current = reconnectDown(Date.now());
      setPhase('reconnecting');
    }
    let timer = 0;
    const arm = () => {
      const due = reconnectWake(machine.current);
      if (due == null) return;
      timer = window.setTimeout(() => {
        if (generation.current !== gen) return;
        const step = reconnectAdvance(machine.current, Date.now());
        machine.current = step.state;
        setPhase(step.state.phase);
        if (step.attempt) void probe.current();
        arm();
      }, Math.max(0, due - Date.now()));
    };
    arm();
    return () => {
      generation.current += 1;
      window.clearTimeout(timer);
    };
  }, [active]);

  const retry = () => {
    const step = reconnectRetry(machine.current);
    if (!step.attempt) return;
    machine.current = step.state;
    setPhase(step.state.phase);
    const gen = generation.current;
    void Promise.resolve(probe.current()).then(ok => {
      if (generation.current !== gen || machine.current.phase !== 'probing') return;
      machine.current = reconnectSettled(machine.current, ok === true);
      setPhase(machine.current.phase);
    });
  };

  return { phase, retry };
}

/**
 * One muted line under the header (I-IFL-3/5/6). Reconnecting while probes
 * are still inside 30s; "Can't reach the engine" and a quiet Retry after
 * that. Nothing here is red. The line is absent while the engine answers.
 */
export function EngineNotice({ active, onProbe }: Props) {
  const { phase, retry } = useReconnectNotice(active, onProbe);
  if (phase === 'online') return null;
  const reconnecting = phase === 'reconnecting' || phase === 'probing';
  return (
    <p className="engine-notice" role="status" data-engine-notice={reconnecting ? 'reconnecting' : 'unreachable'}>
      <span>{reconnecting ? RECONNECTING_NOTICE : UNREACHABLE_NOTICE}</span>
      {!reconnecting && (
        <>
          <span className="engine-notice-dot" aria-hidden="true">·</span>
          <Button variant="ghost" className="engine-notice-retry" onClick={retry}>Retry</Button>
        </>
      )}
    </p>
  );
}

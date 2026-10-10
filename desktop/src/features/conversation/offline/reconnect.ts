/**
 * The conversation's engine line (Interactions Flows, I-IFL-3/5/6).
 *
 * Pure on purpose: the caller supplies the clock and performs the probe this
 * machine asks for. A test advances `now` and never sleeps. Nothing here
 * touches the network, the DOM, or a timer.
 */

/** Shown while automatic probes are still inside the window. The ellipsis is one character, as the design writes it. */
export const RECONNECTING_NOTICE = 'Reconnecting to the engine…';

/** Shown once the window has passed. Retry is a separate quiet button, not part of this string. */
export const UNREACHABLE_NOTICE = "Can't reach the engine";

/** Automatic probes stop and Retry appears once the outage has lasted this long (I-IFL-5). */
export const RECONNECT_WINDOW_MS = 30_000;

/**
 * Wait before each automatic probe, counted from the previous one. The last
 * delay repeats. These are the steps the conversation already used to
 * reattach, so the line and the probes share one schedule. The design names
 * the 30s window and not these steps.
 */
export const RECONNECT_BACKOFF_MS = [1_000, 2_000, 5_000, 10_000] as const;

export type ReconnectPhase = 'online' | 'reconnecting' | 'probing' | 'unreachable';

export type ReconnectState = {
  phase: ReconnectPhase;
  /** Epoch ms the outage began. Absent while the engine answers. */
  since: number | null;
  /** Automatic probes already handed to the caller for this outage. */
  attempts: number;
  /** Epoch ms of the next automatic probe. Absent when none is scheduled. */
  nextAttemptAt: number | null;
};

export function reconnectOnline(): ReconnectState {
  return { phase: 'online', since: null, attempts: 0, nextAttemptAt: null };
}

/** Delay before automatic probe number `attempt`, counting from zero. */
export function backoffDelay(attempt: number): number {
  return RECONNECT_BACKOFF_MS[Math.min(Math.max(attempt, 0), RECONNECT_BACKOFF_MS.length - 1)];
}

/** The engine stopped answering at `now`. The first probe waits out the first backoff. */
export function reconnectDown(now: number): ReconnectState {
  return { phase: 'reconnecting', since: now, attempts: 0, nextAttemptAt: now + backoffDelay(0) };
}

/**
 * When the caller should next advance the machine. The window's end is a wake
 * even when no probe remains, so the copy can change without a burst of tries.
 * Null while the line will not change on its own (online, probing, or Retry).
 */
export function reconnectWake(state: ReconnectState): number | null {
  if (state.phase !== 'reconnecting' || state.since == null) return null;
  const end = state.since + RECONNECT_WINDOW_MS;
  if (state.nextAttemptAt == null || state.nextAttemptAt >= end) return end;
  return state.nextAttemptAt;
}

export type ReconnectStep = { state: ReconnectState; attempt: boolean };

/**
 * Move the machine to `now`. At most one probe is reported, and a jump past
 * the window reports none: giving up must not fire every missed backoff.
 */
export function reconnectAdvance(state: ReconnectState, now: number): ReconnectStep {
  if (state.phase !== 'reconnecting' || state.since == null) return { state, attempt: false };
  const end = state.since + RECONNECT_WINDOW_MS;
  if (now >= end) return { state: { ...state, phase: 'unreachable', nextAttemptAt: null }, attempt: false };
  if (state.nextAttemptAt == null || now < state.nextAttemptAt) return { state, attempt: false };
  const attempts = state.attempts + 1;
  const next = now + backoffDelay(attempts);
  return {
    state: { phase: 'reconnecting', since: state.since, attempts, nextAttemptAt: next < end ? next : null },
    attempt: true,
  };
}

/**
 * Retry, pressed once the window has passed. One probe starts immediately and
 * the line says it is trying until that probe settles. The 30s window does
 * not start over: a miss puts Retry back.
 */
export function reconnectRetry(state: ReconnectState): ReconnectStep {
  if (state.phase !== 'unreachable') return { state, attempt: false };
  return { state: { ...state, phase: 'probing', nextAttemptAt: null }, attempt: true };
}

/** The probe reconnectRetry asked for has finished. Success clears the line. */
export function reconnectSettled(state: ReconnectState, ok: boolean): ReconnectState {
  if (ok) return reconnectOnline();
  if (state.phase === 'probing') return { ...state, phase: 'unreachable', nextAttemptAt: null };
  return state;
}

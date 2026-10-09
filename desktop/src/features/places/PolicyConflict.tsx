import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Button, Icon, StatusMark, Tag } from '../../components/ui';
import { applyOffer, fieldLabel, isConflict, placeName, settingWords, valueLabel, wantedLine } from './using-model';
import type { PlaceSetting, PolicyDecision, PolicyField, SettingState, UsingBundle } from './using-types';

type RowProps = {
  field: PolicyField;
  bundle: UsingBundle;
  decision?: PolicyDecision;
  setting?: PlaceSetting;
  busy: boolean;
  /** Remembers which place wins this field for this conversation. Resolves true when the engine accepted it. */
  onChoose: (placeId: string) => Promise<boolean>;
  /** The person takes the places' value as this conversation's own. Resolves true when the engine accepted it. */
  onApply: () => Promise<boolean>;
};

/** The two states that wait on the person carry the shared "your call" dot; the word beside it says what to do. */
const waiting: ReadonlySet<SettingState> = new Set(['needsPick', 'needsYou']);

/**
 * Which place wins when places disagree (Places 6e: "the nearest common ancestor decides; with none, the chat asks once and
 * remembers"). Picking POSTs the choice to the engine. It is remembered for this conversation, and it is applied at the next
 * turn: the buttons never say the model changed, because only the engine's own state word can.
 */
export function PolicyConflict({ field, bundle, decision, busy, onChoose }: Pick<RowProps, 'field' | 'bundle' | 'busy' | 'onChoose'> & { decision: PolicyDecision }) {
  const candidates = decision.wanted.filter((want, index) => decision.wanted.findIndex(other => other.placeId === want.placeId) === index);
  const choosing = decision.outcome === 'needsPick';
  const word = fieldLabel[field].toLowerCase();
  return (
    <div className="using-conflict" role="group" aria-label={`Which place decides the ${word}`}>
      <p className="using-note">
        {choosing ? `These places want a different ${word}. Pick the one this conversation follows. ` : `You chose which place decides the ${word}. `}
        Asked once; the answer is remembered for this conversation.
      </p>
      <div className="using-choices">
        {candidates.map(want => {
          const chosen = decision.outcome === 'chosen' && decision.chosen === want.placeId;
          return (
            <Button
              key={want.placeId}
              variant="quiet"
              className="using-choice"
              aria-pressed={chosen}
              loading={busy}
              onClick={() => { if (!chosen) void onChoose(want.placeId); }}
            >
              <span>{placeName(bundle, want.placeId)}</span>
              <span className="using-choice-value">{valueLabel(field, want.value)}</span>
            </Button>
          );
        })}
      </div>
    </div>
  );
}

/**
 * Taking a place's value as the person's own. A wider permissions setting is held until this, and it takes two deliberate
 * steps: the button asks, and a second control confirms, so a stray key or click can never widen what a conversation may do.
 */
function Apply({ field, setting, busy, onApply }: Pick<RowProps, 'field' | 'busy' | 'onApply'> & { setting: PlaceSetting }) {
  const offer = applyOffer(setting);
  const [confirming, setConfirming] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const confirm = useRef<HTMLButtonElement>(null);
  useEffect(() => { if (confirming) confirm.current?.focus(); }, [confirming]);
  if (!offer || !setting.value) return null;
  const value = valueLabel(field, setting.value);
  const cancel = () => { setConfirming(false); requestAnimationFrame(() => trigger.current?.focus()); };
  const keys = (event: KeyboardEvent) => {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    event.stopPropagation();
    cancel();
  };
  if (confirming) {
    return (
      <div className="using-confirm" role="group" aria-label={`Confirm ${fieldLabel[field].toLowerCase()}`} onKeyDown={keys}>
        <p className="using-note">
          {setting.current ? `Change ${fieldLabel[field].toLowerCase()} here from ${valueLabel(field, setting.current)} to ${value}?` : `Use ${value} for ${fieldLabel[field].toLowerCase()} here?`} Only this conversation changes.
        </p>
        <div className="using-confirm-actions">
          <Button ref={confirm} variant="primary" loading={busy} onClick={() => { void onApply().then(done => { if (done) setConfirming(false); }); }}>Use {value}</Button>
          <Button variant="quiet" disabled={busy} onClick={cancel}>Cancel</Button>
        </div>
      </div>
    );
  }
  return (
    <div className="using-apply">
      <Button
        ref={trigger}
        variant="quiet"
        loading={busy}
        onClick={() => { if (offer.confirm) setConfirming(true); else void onApply(); }}
      >
        Use {value} in this chat{offer.confirm ? '…' : ''}
      </Button>
    </div>
  );
}

/**
 * One policy field: what the places decided, and what this conversation is doing about it, in the engine's own state word.
 * Model and permissions are separate rows because the engine decides them separately.
 */
export function PolicyRow({ field, bundle, decision, setting, busy, onChoose, onApply }: RowProps) {
  const value = setting?.value || decision?.value || '';
  const aside = decision ? wantedLine(decision, bundle) : undefined;
  const running = setting?.current && setting.current !== setting.value && setting.state !== 'applied' ? setting.current : undefined;
  return (
    <li className="using-row using-policy" data-kind="policy" data-field={field} data-state={setting?.state}>
      <div className="using-row-main using-policy-head">
        <Icon name={field === 'model' ? 'cpu' : 'shield'} size="sm" />
        <span className="using-policy-title">{fieldLabel[field]}{value ? `: ${valueLabel(field, value)}` : ''}</span>
        {aside && <span className="using-row-aside">{aside}</span>}
      </div>
      {setting && (
        <div className="using-policy-state">
          {waiting.has(setting.state) && <span aria-hidden="true"><StatusMark dense status="waiting" label={settingWords[setting.state]} /></span>}
          <Tag tone="neutral">{settingWords[setting.state]}</Tag>
          {(setting.reason || running) && (
            <span className="using-note">{setting.reason}{setting.reason && running ? ' ' : ''}{running ? `Running on ${valueLabel(field, running)} now.` : ''}</span>
          )}
        </div>
      )}
      {decision && isConflict(decision) && <PolicyConflict field={field} bundle={bundle} decision={decision} busy={busy} onChoose={onChoose} />}
      {setting && <Apply field={field} setting={setting} busy={busy} onApply={onApply} />}
    </li>
  );
}

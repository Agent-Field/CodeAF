import { Button, Select, Text, TextArea, TextInput } from '../../../components/ui';
import { pairsEither, togglePicked, type Draft } from './answers';
import { sentenceStart, visibleOptions, type Question } from './form';

export type FormProps = {
  question: Question;
  draft: Draft;
  edit: (change: Partial<Draft>) => void;
  locked: boolean;
};

type Blank = NonNullable<NonNullable<Question['input']>['blanks']>[number];

function timeType(blank: Blank) {
  return !blank.default || /^\d{1,2}:\d{2}/.test(blank.default) ? 'time' : 'text';
}

function inputType(blank: Blank, secret: boolean) {
  if (blank.kind === 'number') return 'number';
  if (blank.kind === 'time') return timeType(blank);
  return secret ? 'password' : 'text';
}

export function BlankFields({ question, draft, edit, locked }: FormProps) {
  const blanks = question.input?.blanks ?? [];
  const set = (label: string, value: string) => edit({ blanks: { ...draft.blanks, [label]: value } });
  return (
    <div className="tray-fields">
      {blanks.map((blank) => (
        <label key={blank.label} className="tray-field-row">
          <span className="tray-caption">{blank.label}</span>
          {blank.kind === 'choice' ? (
            <Select
              label={blank.label}
              disabled={locked}
              value={draft.blanks[blank.label] ?? ''}
              onValueChange={(value) => set(blank.label, value)}
              options={(blank.choices ?? []).map((value) => ({ value, label: value }))}
            />
          ) : (
            <TextInput
              className={`tray-field ${blank.kind === 'path' ? 'tray-mono' : ''}`}
              type={inputType(blank, Boolean(question.input?.secret))}
              autoComplete="off"
              disabled={locked}
              value={draft.blanks[blank.label] ?? ''}
              onChange={(event) => set(blank.label, event.target.value)}
            />
          )}
        </label>
      ))}
    </div>
  );
}

export function PairRows({ question, draft, edit, locked }: FormProps) {
  const set = (label: string, side: string) => edit({ blanks: { ...draft.blanks, [label]: side } });
  return (
    <div className="tray-fields">
      {(question.input?.blanks ?? []).map((blank) => {
        const [left = '', right = ''] = blank.choices ?? [];
        const sides = [left, pairsEither, right];
        return (
          <div key={blank.label} className="tray-pair" role="group" aria-label={blank.label}>
            <span className="tray-caption">{blank.label}</span>
            <div className="tray-segment">
              {sides.map((side) => (
                <Button
                  key={side}
                  variant={draft.blanks[blank.label] === side ? 'raised' : 'ghost'}
                  aria-pressed={draft.blanks[blank.label] === side}
                  disabled={locked}
                  onClick={() => set(blank.label, side)}
                >
                  {side === pairsEither ? 'Either' : side}
                </Button>
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}

export function DialField({ question, draft, edit, locked }: FormProps) {
  const dial = question.input?.dial;
  if (!dial) return null;
  const whole = [dial.min, dial.max, dial.default].every(Number.isInteger);
  const labels = dial.labels?.length ? dial.labels : [String(dial.min), String(dial.max)];
  return (
    <div className="tray-dial">
      <TextInput
        type="range"
        aria-label={question.input?.prompt || question.head}
        min={dial.min}
        max={dial.max}
        step={whole ? 1 : (dial.max - dial.min) / 100}
        disabled={locked}
        value={draft.dial ?? dial.default}
        onChange={(event) => edit({ dial: Number(event.target.value) })}
      />
      <div className="tray-dial-labels">
        {labels.map((label, index) => <span key={index} className="tray-caption">{label}</span>)}
      </div>
      <Text className="tray-caption">
        {draft.dial ?? dial.default}
        {draft.dial !== dial.default && ` · suggested ${dial.default}`}
      </Text>
    </div>
  );
}

export function CheckList({ question, draft, edit, locked }: FormProps) {
  return (
    <ul className="tray-checks">
      {(question.options ?? []).map((option) => (
        <li key={option.key}>
          <label className="tray-check">
            <TextInput
              type="checkbox"
              disabled={locked}
              checked={draft.picked.includes(option.key)}
              onChange={() => edit({ picked: togglePicked(draft.picked, option.key) })}
            />
            <span>
              {option.label}
              {option.body && <span className="tray-caption"> {option.body}</span>}
            </span>
          </label>
        </li>
      ))}
    </ul>
  );
}

type TextProps = FormProps & { onEnter: () => void };

/** Free words. A secret is a password field: masked, never echoed, never kept after sending. */
export function WordsField({ question, draft, edit, locked, onEnter }: TextProps) {
  const prompt = question.input?.prompt || 'Your answer';
  if (question.input?.secret) {
    return (
      <TextInput
        className="tray-field"
        type="password"
        autoComplete="off"
        aria-label={prompt}
        placeholder={prompt}
        disabled={locked}
        value={draft.change}
        onChange={(event) => edit({ change: event.target.value })}
        onKeyDown={(event) => event.key === 'Enter' && onEnter()}
      />
    );
  }
  return (
    <TextArea
      className="tray-field"
      rows={2}
      aria-label={prompt}
      placeholder={prompt}
      disabled={locked}
      value={draft.change}
      onChange={(event) => edit({ change: event.target.value })}
    />
  );
}

/** Quiet ways out of an input form: "Not now", "Skip". */
export function Declines({ question, locked, onPress }: { question: Question; locked: boolean; onPress: (key: string) => void }) {
  return (
    <>
      {visibleOptions(question)
        .filter((option) => option.safe)
        .map((option) => (
          <Button key={option.key} disabled={locked} onClick={() => onPress(option.key)}>
            {sentenceStart(option.label)}
          </Button>
        ))}
    </>
  );
}

import { Button, Text, TextArea } from '../../../components/ui';
import { CardEvidence, type RenderImage } from './CardEvidence';
import { optionLabel, optionVariant, scopeLabel, visibleOptions, widerScopes, type Option, type Question } from './form';

type ListProps = {
  question: Question;
  locked: boolean;
  onPress: (option: Option) => void;
  renderImage?: RenderImage;
};

const confidenceNote = (confidence?: string) => (confidence === 'unsure' ? ' · not sure' : '');

function Detail({ option }: { option: Option }) {
  return (
    <>
      {option.body && <Text className="tray-caption">{option.body}</Text>}
      {option.consequence && <Text className="tray-caption">{option.consequence}</Text>}
    </>
  );
}

function isPlain(options: Option[]) {
  return options.every((option) => !option.body && !option.consequence && !option.blocks?.length);
}

/** Short answers sit in one row of buttons; answers with something to read stack. */
export function OptionList({ question, locked, onPress, renderImage }: ListProps) {
  const options = visibleOptions(question);
  const pick = question.pick;
  if (!options.length) return null;
  const button = (option: Option) => (
    <Button key={option.key} variant={optionVariant(question, option)} disabled={locked} onClick={() => onPress(option)}>
      {optionLabel(question, option)}
    </Button>
  );
  if (isPlain(options)) {
    const picked = options.find((option) => option.key === pick?.key);
    return (
      <>
        <div className="tray-actions">{options.map(button)}</div>
        {picked && pick?.reason && (
          <Text className="tray-caption">{`Suggested: ${optionLabel(question, picked)}. ${pick.reason}`}</Text>
        )}
      </>
    );
  }
  return (
    <ul className="tray-options">
      {options.map((option) => (
        <li key={option.key} className="tray-option">
          <div className="tray-option-head">
            {button(option)}
            {option.key === pick?.key && <span className="tray-badge">{`Suggested${confidenceNote(pick?.confidence)}`}</span>}
          </div>
          <Detail option={option} />
          {option.key === pick?.key && pick?.reason && <Text className="tray-caption">{pick.reason}</Text>}
          <CardEvidence blocks={option.blocks} renderImage={renderImage} />
        </li>
      ))}
    </ul>
  );
}

/** "Always…": how wide the yes reaches. */
export function ScopeChoice({ question, locked, onPick }: { question: Question; locked: boolean; onPick: (scope: string) => void }) {
  return (
    <div className="tray-panel" role="group" aria-label="How far this yes reaches">
      {widerScopes(question).map((scope) => (
        <Button key={scope} variant="quiet" disabled={locked} onClick={() => onPick(scope)}>
          {scopeLabel(scope)}
        </Button>
      ))}
    </div>
  );
}

type WordsProps = { prompt: string; value: string; locked: boolean; onChange: (value: string) => void; onSend: () => void };

/** Words that must be written before this answer can go ("Tell it…", "add more"). */
export function WordsPanel({ prompt, value, locked, onChange, onSend }: WordsProps) {
  return (
    <div className="tray-panel tray-panel-column">
      <TextArea
        className="tray-field"
        rows={2}
        autoFocus
        aria-label={prompt}
        placeholder={prompt}
        disabled={locked}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      <Button variant="primary" disabled={locked || value.trim() === ''} onClick={onSend}>
        Send
      </Button>
    </div>
  );
}

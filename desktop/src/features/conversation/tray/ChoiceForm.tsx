import { useState } from 'react';
import { Button, Text, TextInput } from '../../../components/ui';
import { CardEvidence, type RenderImage } from './CardEvidence';
import { deadlineAt, span } from './clock';
import { formOf, optionLabel, visibleOptions, type Option, type Question } from './form';
import './choice.css';

type Props = {
  question: Question;
  now: number;
  held: boolean;
  locked: boolean;
  onChoose: (option: Option) => void;
  onHold: () => void;
  renderImage?: RenderImage;
};

/** Options with something to read are cards; short answers stay a row of buttons. */
export function wantsCards(question: Question): boolean {
  if (formOf(question) !== 'choice') return false;
  return visibleOptions(question).some((option) => option.body || option.consequence || option.blocks?.length);
}

type CardProps = {
  question: Question;
  option: Option;
  chosen: boolean;
  locked: boolean;
  onSelect: () => void;
  renderImage?: RenderImage;
};

function Card({ question, option, chosen, locked, onSelect, renderImage }: CardProps) {
  const pick = question.pick;
  const suggested = option.key === pick?.key;
  return (
    <label className={`choice-card ${chosen ? 'choice-card-chosen' : ''}`} title={suggested ? pick?.reason : undefined}>
      <TextInput
        className="choice-radio"
        type="radio"
        name={`choice-${question.id}`}
        disabled={locked}
        checked={chosen}
        onChange={onSelect}
      />
      <span className="choice-title">
        {optionLabel(question, option)}
        {suggested && <span className="choice-tag">Suggested</span>}
      </span>
      {option.body && <span className="choice-detail">{option.body}</span>}
      {option.consequence && <span className="choice-detail">{option.consequence}</span>}
      <CardEvidence blocks={option.blocks} renderImage={renderImage} />
    </label>
  );
}

/** What happens when the clock runs out, said aloud; a held clock says it is stopped. */
function Countdown({ question, now, held }: { question: Question; now: number; held: boolean }) {
  const at = deadlineAt(question);
  if (at === null) return null;
  const left = span(Math.max(0, Math.ceil((at - now) / 1000)));
  return <Text className="choice-clock" role="timer">{held ? 'On hold — take your time' : `Picks Suggested in ${left}`}</Text>;
}

/** Cards to pick from, the engine's pick lit first, and Choose. A Deadline adds the clock and Hold. */
export function ChoiceForm({ question, now, held, locked, onChoose, onHold, renderImage }: Props) {
  const options = visibleOptions(question);
  const [key, setKey] = useState(() => (options.some((o) => o.key === question.pick?.key) ? question.pick?.key : undefined));
  const chosen = options.find((option) => option.key === key);
  const clocked = deadlineAt(question) !== null;
  return (
    <>
      <div className="choice-cards" role="radiogroup" aria-label={question.head}>
        {options.map((option) => (
          <Card
            key={option.key}
            question={question}
            option={option}
            chosen={option.key === key}
            locked={locked}
            onSelect={() => setKey(option.key)}
            renderImage={renderImage}
          />
        ))}
      </div>
      <div className="choice-actions">
        <Button variant="primary" className="choice-choose" disabled={locked || !chosen} onClick={() => chosen && onChoose(chosen)}>
          Choose
        </Button>
        <Countdown question={question} now={now} held={held} />
        {clocked && !held && <Button className="choice-hold" disabled={locked} onClick={onHold}>Hold</Button>}
      </div>
    </>
  );
}

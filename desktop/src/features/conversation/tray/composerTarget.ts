import type { EngineAnswer } from '../../chat/engine-client.ts';
import { answerFor, initialDraft, submitAnswer } from './answers.ts';
import { formOf, needsWords, type Option, type Question } from './form.ts';
import { questionKey } from './layout.ts';

export type ComposerTarget = { key: string; title: string; placeholder: string; answer: (text: string) => EngineAnswer };

/** One target describes the words a front card accepts, so the composer never branches on card kind. */
export function composerTarget(question: Question | undefined, wordsOption?: Option): ComposerTarget | null {
  if (!question) return null;
  const options = question.options ?? [];
  const review = formOf(question) === 'landing';
  const option = wordsOption ?? (review ? options.find(option => option.key === 'n') ?? options.find(option => needsWords(question, option))
    : options.find(option => needsWords(question, option) || /^other(?:…|\.\.\.)?$/i.test(option.label.trim())) ?? (question.input?.kind === 'text' ? options.find(option => !option.safe && !option.widening) : undefined));
  if (!option && formOf(question) !== 'text') return null;
  return {
    key: questionKey(question), title: question.asker?.kind === 'task' ? question.asker.name || question.head : question.head,
    placeholder: wordsOption?.safe && formOf(question) === 'permission' ? 'Say why (optional)' : review ? 'Tell it what to change' : question.input?.prompt && question.input.prompt !== 'Your answer' ? question.input.prompt : question.head,
    answer: text => {
      const draft = { ...initialDraft(question), change: text };
      return option ? answerFor(question, draft, option) : submitAnswer(question, draft);
    },
  };
}

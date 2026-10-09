import { Text } from '../../components/ui';

/** Rows past this many are left out of the DOM; the engine reads up to 1MB, which can be tens of thousands of lines. */
export const lineLimit = 5000;

/** The text of a file split into lines; one trailing newline does not make an extra empty line. */
export const linesOf = (text: string): string[] => {
  const lines = text.split('\n');
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop();
  return lines;
};

/** The whole file: a line-number column and the text in mono, in ink. */
export function FileLines({ lines }: { lines: readonly string[] }) {
  const shown = lines.length > lineLimit ? lines.slice(0, lineLimit) : lines;
  return <>
    {shown.map((text, index) => <div key={index} className="file-row file-plain-row">
      <span className="file-num">{index + 1}</span>
      <span className="file-text">{text}</span>
    </div>)}
    {lines.length > lineLimit && <Text className="file-note">Showing the first {lineLimit} of {lines.length} lines.</Text>}
  </>;
}

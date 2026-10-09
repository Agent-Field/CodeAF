import { useState } from 'react';
import { Button, CodeText } from '../../../components/ui';
import { splitLines } from './diff';

/** Mono excerpt of the first `rows` lines, with the rest one click away. Renders nothing when empty. */
export function LinesBlock({ text, rows, label }: { text: string; rows: number; label: string }) {
  const [all, setAll] = useState(false);
  const lines = splitLines(text);
  if (lines.length === 0) return null;
  const shown = all ? lines : lines.slice(0, rows);
  return (
    <div className="work-excerpt">
      <pre className="work-pre" aria-label={label}>
        <CodeText>{shown.join('\n')}</CodeText>
      </pre>
      {lines.length > shown.length && (
        <Button className="work-more" onClick={() => setAll(true)}>
          {`Show all ${lines.length} lines`}
        </Button>
      )}
    </div>
  );
}

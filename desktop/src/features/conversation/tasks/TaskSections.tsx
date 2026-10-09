import type { ReactNode } from 'react';
import { CodeText, Icon, Markdown } from '../../../components/ui';

export type RenderFile = (path: string) => ReactNode;

/** What the task produced, first: it is what the person came for. */
export function Result({ text }: { text: string }) {
  if (!text.trim()) return null;
  return (
    <section className="task-view-result" aria-label="Result">
      <Markdown>{text}</Markdown>
    </section>
  );
}

/** The worker's last words, for a task that ended without a result. */
export function LastWords({ text, shown }: { text: string; shown: boolean }) {
  if (!shown || !text.trim()) return null;
  return <blockquote className="task-view-last">{text}</blockquote>;
}

/** Files are chips when the app can draw them, plain paths when it cannot. */
export function ChangedFiles({ paths, renderFile }: { paths: string[]; renderFile?: RenderFile }) {
  if (paths.length === 0) return null;
  return (
    <section className="task-view-changed" aria-label="Changed files">
      <h3 className="task-view-eyebrow">Changed files</h3>
      <ul className="task-view-files">
        {paths.map((path) => (
          <li key={path}>{renderFile ? renderFile(path) : <CodeText>{path}</CodeText>}</li>
        ))}
      </ul>
    </section>
  );
}

export function Checks({ checks }: { checks: string[] }) {
  if (checks.length === 0) return null;
  return (
    <ul className="task-view-list" aria-label="Checks">
      {checks.map((check, index) => (
        <li key={index} className="task-view-check">
          <Icon name="checklist" size="xs" />
          <span>{check}</span>
        </li>
      ))}
    </ul>
  );
}

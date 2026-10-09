import type { ReactNode } from 'react';
import { Text } from '../../../components/ui';
import type { EngineQuestionBlock } from '../../chat/engine-client';
import { QuestionEvidence } from '../QuestionEvidence';
import { compareRows, type Question } from './form';
import '../notice.css';

export type RenderImage = (block: EngineQuestionBlock) => ReactNode;

/** Evidence the asker attached. Pictures are drawn by the host; without one, the path is named. */
export function CardEvidence({ blocks, renderImage }: { blocks?: EngineQuestionBlock[]; renderImage?: RenderImage }) {
  if (!blocks?.length) return null;
  return (
    <div className="tray-evidence">
      {blocks.map((block, index) => {
        const picture = block.kind === 'image' ? renderImage?.(block) : null;
        return picture ? (
          <figure key={index} className="tray-figure">
            {picture}
            {block.title && <figcaption className="tray-caption">{block.title}</figcaption>}
          </figure>
        ) : (
          <QuestionEvidence key={index} block={block} />
        );
      })}
    </div>
  );
}

/** Options that share comparison axes are laid side by side. */
export function CompareTable({ question }: { question: Question }) {
  const rows = compareRows(question);
  if (!rows) return null;
  const [head, ...body] = rows;
  return (
    <div className="tray-compare">
      <table>
        <thead>
          <tr>{head.map((cell, index) => <th key={index}>{cell}</th>)}</tr>
        </thead>
        <tbody>
          {body.map((row) => (
            <tr key={row[0]}>
              {row.map((cell, column) => (column === 0 ? <th key={column} scope="row">{cell}</th> : <td key={column}>{cell}</td>))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function Caption({ children }: { children: ReactNode }) {
  return <Text className="tray-caption">{children}</Text>;
}

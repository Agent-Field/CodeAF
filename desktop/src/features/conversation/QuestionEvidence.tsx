import { Text } from '../../components/ui';
import type { EngineQuestionBlock } from '../chat/engine-client';

const CODE_BLOCKS = new Set(['code', 'diff', 'diagram', 'layout']);

function Table({ rows }: { rows: string[][] }) {
  const [head, ...body] = rows;
  return (
    <div className="question-table">
      <table>
        <thead>
          <tr>{head.map((cell, index) => <th key={index}>{cell}</th>)}</tr>
        </thead>
        <tbody>
          {body.map((row, index) => (
            <tr key={index}>{row.map((cell, column) => <td key={column}>{cell}</td>)}</tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Evidence the engine attached to a question or an option, drawn literally. */
export function QuestionEvidence({ block }: { block: EngineQuestionBlock }) {
  const code = CODE_BLOCKS.has(block.kind);
  return (
    <div className="question-evidence">
      {block.title && <Text tone="default">{block.title}</Text>}
      {block.body && (code ? <pre className="question-code">{block.body}</pre> : <Text>{block.body}</Text>)}
      {block.kind === 'table' && block.rows?.length ? <Table rows={block.rows} /> : null}
      {block.path && <Text className="question-path">{block.path}</Text>}
    </div>
  );
}

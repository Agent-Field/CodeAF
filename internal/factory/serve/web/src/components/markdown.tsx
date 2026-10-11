import ReactMarkdown from "react-markdown"

// Results and thread messages are markdown from a model. react-markdown
// renders no raw HTML, so a result cannot inject markup; links leave the page.
export function Markdown({ children }: { children: string }) {
  return (
    <div className="text-sm leading-relaxed break-words [&_a]:underline [&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_code]:font-mono [&_code]:text-xs [&_h1]:text-base [&_h1]:font-semibold [&_h2]:font-semibold [&_h3]:font-medium [&_li]:ml-4 [&_ol]:list-decimal [&_p]:my-1.5 [&_pre]:overflow-x-auto [&_pre]:rounded [&_pre]:bg-muted [&_pre]:p-2 [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_ul]:list-disc">
      <ReactMarkdown
        components={{ a: ({ node: _node, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer" /> }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}

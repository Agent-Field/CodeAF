package tool

const grepDescription = "- Fast content search tool that works with any codebase size\n" +
	"- Searches file contents using regular expressions\n" +
	"- Supports full regex syntax (eg. \"log.*Error\", \"function\\s+\\w+\", etc.)\n" +
	"- Filter files by pattern with the include parameter (eg. \"*.js\", \"*.{ts,tsx}\")\n" +
	"- Returns file paths and line numbers with at least one match sorted by modification time\n" +
	"- Use this tool when you need to find files containing specific patterns\n" +
	"- If you need to identify/count the number of matches within files, use the Bash tool with `rg` (ripgrep) directly. Do NOT use `grep`.\n" +
	"- When you are doing an open-ended search that may require multiple rounds of globbing and grepping, use the Task tool instead\n"

package tool

const readDescription = "Read a file or directory from the local filesystem. If the path does not exist, an error is returned.\n" +
	"\n" +
	"Usage:\n" +
	"- The filePath parameter should be an absolute path.\n" +
	"- By default, this tool returns up to 2000 lines from the start of the file.\n" +
	"- The offset parameter is the line number to start from (1-indexed).\n" +
	"- To read later sections, call this tool again with a larger offset.\n" +
	"- Use the grep tool to find specific content in large files or files with long lines.\n" +
	"- If you are unsure of the correct file path, use the glob tool to look up filenames by glob pattern.\n" +
	"- Contents are returned with each line prefixed by its line number as `<line>: <content>`. For example, if a file has contents \"foo\\n\", you will receive \"1: foo\\n\". For directories, entries are returned one per line (without line numbers) with a trailing `/` for subdirectories.\n" +
	"- Any line longer than 2000 characters is truncated.\n" +
	"- Call this tool in parallel when you know there are multiple files you want to read.\n" +
	"- Avoid tiny repeated slices (30 line chunks). If you need more context, read a larger window.\n" +
	"- This tool can read image files and PDFs and return them as file attachments.\n"

const writeDescription = `Writes a file to the local filesystem.

Usage:
- This tool will overwrite the existing file if there is one at the provided path.
- If this is an existing file, you MUST use the Read tool first to read the file's contents. This tool will fail if you did not read the file first.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.
- Only use emojis if the user explicitly requests it. Avoid writing emojis to files unless asked.
`

const editDescription = "Performs exact string replacements in files. \n" +
	"\n" +
	"Usage:\n" +
	"- You must use your `Read` tool at least once in the conversation before editing. This tool will error if you attempt an edit without reading the file. \n" +
	"- When editing text from Read tool output, ensure you preserve the exact indentation (tabs/spaces) as it appears AFTER the line number prefix. The line number prefix format is: line number + colon + space (e.g., `1: `). Everything after that space is the actual file content to match. Never include any part of the line number prefix in the oldString or newString.\n" +
	"- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.\n" +
	"- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.\n" +
	"- The edit will FAIL if `oldString` is not found in the file with an error \"oldString not found in content\".\n" +
	"- The edit will FAIL if `oldString` is found multiple times in the file with an error \"Found multiple matches for oldString. Provide more surrounding lines in oldString to identify the correct match.\" Either provide a larger string with more surrounding context to make it unique or use `replaceAll` to change every instance of `oldString`. \n" +
	"- Use `replaceAll` for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.\n"

package programguide

// Sec is the guide sec gives the conversation. Its brief
// is read by the program itself (internal/secaf's scope reading), so the guide
// spells the brief's few words exactly as the program reads them.
const Sec = "For a security audit of code: the whole repository, or only the changes on this branch. " +
	"It hunts for vulnerabilities, traces each from input to sink, sets aside what it cannot show exploitable, " +
	"and reports the rest with file, line and fix; it changes no files. Its brief is `whole repository` or " +
	"`changes` (`changes since <ref>` for another base), optionally with `quick` or `thorough`."

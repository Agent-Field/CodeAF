package programguide

// Review is the guide review gives the conversation. Its brief is read by the
// program itself (internal/praf's ReadBrief), so the guide spells it as the
// program reads it.
const Review = "For a code review of a GitHub pull request; it changes no files and posts nothing. Its brief is " +
	"the pull request (link, `owner/repo#N` or `#N`), or nothing for this branch's, then any focus."

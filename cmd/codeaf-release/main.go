// codeaf-release resolves release tags and channel retention for the release
// workflow.
//
// It is not shipped. Like cmd/codeaf-changes this is a developer's binary and
// never a verb on codeaf, so release policy can be checked without adding to
// the product's SIZE-BUDGET.
//
// Usage:
//
//	codeaf-release next --channel stable|rc [--component patch|minor|major]
//	codeaf-release next --channel dev|staging --sha <commit> --date YYYYMMDD
//	codeaf-release kind <tag>
//	codeaf-release prune --channel dev|staging
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	var err error
	switch args[0] {
	case "next":
		err = runNext(args[1:], stdin, stdout)
	case "kind":
		if len(args) != 2 {
			usage(stderr)
			return 2
		}
		kind := tagKind(args[1])
		fmt.Fprintln(stdout, kind)
		if kind == "other" {
			return 1
		}
		return 0
	case "prune":
		err = runPrune(args[1:], stdin, stdout)
	case "promotion-plan":
		err = runPromotionPlan(args[1:], stdout)
	case "promotion-message":
		err = runPromotionMessage(args[1:], stdout)
	case "promotion-recheck":
		err = runPromotionRecheck(args[1:], stdout)
	case "promotion-release":
		err = runPromotionRelease(args[1:], stdout)
	case "promotion-published":
		err = runPromotionPublished(args[1:], stdout)
	case "promotion-jobs":
		err = runPromotionJobs(args[1:], stdout)
	case "promotion-push-error":
		err = runPromotionPushError(args[1:], stdout)
	case "promotion-decision":
		err = runPromotionDecision(args[1:], stdout)
	default:
		usage(stderr)
		return 2
	}

	if err == nil {
		return 0
	}
	if isUsage(err) {
		fmt.Fprintln(stderr, err)
		usage(stderr)
		return 2
	}
	fmt.Fprintln(stderr, err)
	return 1
}

func usage(w io.Writer) {
	fmt.Fprint(w, `codeaf-release — resolve release tags and channel retention

  next --channel stable|rc [--component patch|minor|major]
      Read existing tags from stdin and print the next semver tag.
  next --channel dev|staging --sha <commit> --date YYYYMMDD
      Print the tag for one channel build.
  kind <tag>
      Print stable, rc, dev, staging, or other.
  prune --channel dev|staging
      Read <tag><tab><RFC3339 createdAt> lines and print expired tags.
  promotion-plan|promotion-decision|promotion-recheck|promotion-release|promotion-published|promotion-jobs|promotion-push-error|promotion-message
      Plan, check, and report the staging promotion. See docs/rules/promotion.md.
`)
}

type usageError struct{ message string }

func (err usageError) Error() string { return err.message }

func usageErr(format string, args ...any) error {
	return usageError{message: fmt.Sprintf(format, args...)}
}

func isUsage(err error) bool {
	_, ok := err.(usageError)
	return ok
}

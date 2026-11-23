package config

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
)

// ── HOW FAR A PERSON WIDENED THE FENCE ─────────────────────────────────────
//
// An unattended run's commands are fenced (internal/executor's fence.go): they
// write only inside the job and cannot read keys and sign-ins or use docker. A
// person widens that in one of three places, and the widening is every word
// any of them says (a list, never a replacement, because each place speaks
// for something different and none of them can narrow the fence):
//
//   - the REPOSITORY: `"jail.allow"` in its .codeaf/config.json, read from the
//     checkout a worktree was cut from (the person's, which a step cannot
//     write) or from the workspace itself. Where the jail can, that folder is
//     read-only to the run, so a step cannot widen its own fence for the next;
//   - the PROFILE: `"jail.allow"` in the person's own config.json, for every
//     unattended run on this machine (a factory's machine says it once);
//   - the ENVIRONMENT: CODEAF_JAIL_ALLOW, for the process that was launched
//     with it, such as a harness that starts `codeaf exec` for a node.
//
// Each is `docker`, `off`, or an absolute path (`~/` is the home folder), as a
// list or one comma-separated string.

// ProjectJailAllow is the key in a project file and the profile.
const ProjectJailAllow = "jail.allow"

// JailAllowEnv is the environment's spelling.
const JailAllowEnv = "CODEAF_JAIL_ALLOW"

// JailAllowAt is every widening word for a run whose repository is repo, under
// profileDir. A project file that is there and cannot be read is an error,
// as every project-layer read is ([LoadProjectConfig]).
func JailAllowAt(repo, profileDir string) ([]string, error) {
	var words []string
	if strings.TrimSpace(repo) != "" {
		project, err := LoadProjectConfig(repo)
		if err != nil {
			return nil, err
		}
		listed, _, err := project.Names(ProjectJailAllow)
		if err != nil {
			return nil, err
		}
		words = append(words, listed...)
	}
	if strings.TrimSpace(profileDir) != "" {
		values, _ := readProfileConfig(profileDir)
		words = append(words, jailWords(values[ProjectJailAllow])...)
	}
	if v := strings.TrimSpace(env.Get(JailAllowEnv)); v != "" {
		words = append(words, v)
	}
	return words, nil
}

// jailWords reads a profile row that is a list or one string.
func jailWords(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var one string
	if json.Unmarshal(raw, &one) == nil && strings.TrimSpace(one) != "" {
		return []string{one}
	}
	return nil
}

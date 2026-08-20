package session

// WHO ELSE IS IN THESE FILES.
//
// Three files in this package already answer three different questions about
// work. task_index.go answers what the project's work CAME TO, taskpresence.go
// answers what every live window is doing AT THIS INSTANT, and taskelsewhere.go
// joins those two for a person sitting inside one session. None of them answers
// the question this file is for, which a person only ever asks about a PLACE:
//
//	who else is writing internal/session/task_index.go right now,
//	and what landed in it while I was not looking?
//
// Both halves became askable the moment a running node started saying which
// paths it had written ([PresenceTask.Files]) and a landed row started naming
// the paths behind its count ([TaskIndexEntry.Files]). This file is the asking,
// written once, so that the surfaces which will want it later cannot each grow
// their own comparison and disagree about who is in a file.
//
// ── TWO LAWS ──
//
//   - EXACT PATHS, NO PATTERNS. Two file lists share a path when they spell it
//     identically. There is no globbing, no case folding and no directory
//     containment, because every path either side comes from one producer
//     (task_run.go's changedPath, which makes them repo-relative and
//     slash-spelled) and a matcher cleverer than its inputs is a matcher that
//     invents overlaps nobody has.
//
//   - ABSENCE IS UNKNOWN, AND IT COMES BACK SEPARATELY. Work that named no
//     files is not work that touched none: it is a node that has not written
//     yet, a session on a build too old to say, or a row from before either
//     field existed. So every question here answers with TWO lists — what does
//     overlap, and what could not be asked — and a caller that ignores the
//     second is a caller reporting "nobody else is in this file" on the strength
//     of a silence. That is the emptiness law, applied where getting it wrong
//     would have two windows writing the same file believing they were alone.
//
// Everything here is pure: lists in, lists out, no clock of its own and no
// reading of disk. A caller supplies the reading it already took.

import (
	"strings"
	"time"
)

// SharedFiles is the paths two sets of files have in common, in the order the
// FIRST set names them, with anything blank and anything repeated dropped.
//
// It is the whole of the comparison, and it is one function so that no surface
// has to spell a nested loop over two path lists again. Either side being empty
// answers nothing at all — see this file's second law for why a caller must not
// read that as "these do not overlap".
func SharedFiles(one, two []string) []string {
	if len(one) == 0 || len(two) == 0 {
		return nil
	}
	want := make(map[string]bool, len(two))
	for _, path := range two {
		if path = strings.TrimSpace(path); path != "" {
			want[path] = true
		}
	}
	var out []string
	seen := make(map[string]bool, len(one))
	for _, path := range one {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] || !want[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// Touching is the work OTHER windows on this project have out that has already
// written one of these files, and — separately — the work that has said nothing
// about files at all.
//
// The two lists are the two honest answers, and neither stands in for the other:
// touching is what is certainly in your way, unknown is what might be and cannot
// be asked. A surface saying "you are alone in this file" may only say it when
// BOTH are empty.
//
// IT IS THE READING'S OWN ROWS AND NOTHING ELSE. An [Elsewhere] has already
// dropped this session and every stale window ([NewElsewhere] applies the
// freshness rule), so a caller cannot smuggle a dead claim past this. The world
// reader's rows reach it the same way: [SessionRow.Presence] values go through
// [NewElsewhere] and come out judged.
//
// NO FILES ASKED IS NO QUESTION ASKED, and it answers nothing rather than
// handing back every window on the project.
func (e Elsewhere) Touching(files []string) (touching, unknown []ElsewhereTask) {
	if len(files) == 0 {
		return nil, nil
	}
	for _, row := range e.rows {
		for _, task := range row.RunningTasks {
			at := ElsewhereTask{
				SessionID: row.SessionID,
				Session:   e.names[row.SessionID],
				Task:      task,
			}
			switch {
			case len(task.Files) == 0:
				unknown = append(unknown, at)
			case len(SharedFiles(task.Files, files)) > 0:
				touching = append(touching, at)
			}
		}
	}
	return touching, unknown
}

// LandedTouching is the work that FINISHED in one of these files since a moment
// — and, separately, the work that finished since then without saying which
// files it wrote.
//
// It is the ground-shift question: a node started against a file an hour ago,
// somebody else's task has landed in that file since, and the node is now
// working from a copy of the world that no longer exists. The answer is a
// citation, which is what [TaskIndexEntry] is for — the caller is expected to
// take the row's TranscriptURI or ArtifactURI and go and look.
//
// rows are the index as [ReadTaskIndex] returned them and the order is carried
// through untouched: newest first, which is the order somebody reading "what has
// happened since" wants.
//
// LIVE ROWS ARE NOT LANDED WORK and are left out of both lists. A row saying
// running is a claim about a process, and the file is the wrong place to ask
// about a process — [Elsewhere.Touching] is the right one, and a row appearing
// in both answers would be one piece of work counted twice.
//
// A ZERO after MEANS THE WHOLE FILE, which is what a caller with no starting
// moment actually wants; the row's EndedAt has to be strictly after it, so a
// caller passing the moment it last looked is not handed back the row it looked
// at.
func LandedTouching(rows []TaskIndexEntry, files []string, after time.Time) (touching, unknown []TaskIndexEntry) {
	if len(rows) == 0 || len(files) == 0 {
		return nil, nil
	}
	for _, row := range rows {
		if row.Live() || row.EndedAt.IsZero() || !row.EndedAt.After(after) {
			continue
		}
		if len(row.Files) == 0 {
			// A row from before this field existed, and a row whose writer never
			// filled it in, are the same silence — and so is a row for a node that
			// genuinely wrote nothing. FilesChanged cannot tell them apart: it is
			// zero for all three (an orchestrated run's rows have always carried no
			// count at all). So the silence is reported as silence.
			unknown = append(unknown, row)
			continue
		}
		if len(SharedFiles(row.Files, files)) > 0 {
			touching = append(touching, row)
		}
	}
	return touching, unknown
}

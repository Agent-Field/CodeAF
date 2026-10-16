package session

import "testing"

// A POST'S ANSWER READS BACK AS THE FACTS THAT WROTE IT, for every shape the
// two writers make, and nothing else reads as a post.
func TestTeamPostAnswerReadsBackAsItsFacts(t *testing.T) {
	cases := []struct {
		said string
		want TeamPost
	}{
		{teamPostSaid("the manager", "Verification", "000000000003", "000000000002"), TeamPost{Team: "Verification", To: "manager", Post: "000000000003", Answers: "000000000002"}},
		{teamPostSaid("the room", `Re "lease", in`, "000000000010", ""), TeamPost{Team: `Re "lease", in`, To: "room", Post: "000000000010"}},
		{teamPostSaid("@backend", "Release", "", ""), TeamPost{Team: "Release", To: "backend"}},
		{teamPostUpSaid("Release", "Docs"), TeamPost{Team: "Release", To: "manager", Up: true, From: "Docs"}},
	}
	for _, c := range cases {
		got, ok := ReadTeamPost(c.said)
		if !ok {
			t.Fatalf("%q did not read back", c.said)
		}
		c.want.Post, c.want.Answers = trimID(c.want.Post), trimID(c.want.Answers)
		got.Post, got.Answers = trimID(got.Post), trimID(got.Answers)
		if got != c.want {
			t.Fatalf("%q read %+v, want %+v", c.said, got, c.want)
		}
	}
	for _, refused := range []string{
		"",
		"No teammate in \"Release\" has the handle \"x\". Post to room, manager, or one of @a.",
		"The post could not be written to the team's traffic: disk full",
		"Posted to the manager in \"Verification\" as #3, answering #2. Something else.",
		"Posted to somebody in \"Verification\". It arrives at the start of their next step.",
	} {
		if post, ok := ReadTeamPost(refused); ok {
			t.Fatalf("%q read as a post %+v", refused, post)
		}
	}
}

// The exact sentence the native fixture recorded reads as post #3 answering #2.
func TestTeamPostRecordedSentenceReads(t *testing.T) {
	post, ok := ReadTeamPost("Posted to the manager in \"Verification\" as #3, answering #2. It arrives at the start of their next step.")
	if !ok || post.To != "manager" || post.Team != "Verification" || trimID(post.Post) != "3" || trimID(post.Answers) != "2" {
		t.Fatalf("read %+v %v", post, ok)
	}
}

func trimID(id string) string {
	for len(id) > 1 && id[0] == '0' {
		id = id[1:]
	}
	return id
}

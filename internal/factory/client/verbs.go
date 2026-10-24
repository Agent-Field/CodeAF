package client

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

func (c *Client) launch(id int) error  { return c.postID(id, "run", nil) }
func (c *Client) stop(id int) error    { return c.postID(id, "stop", nil) }
func (c *Client) dismiss(id int) error { return c.postID(id, "drop", nil) }
func (c *Client) reverify(id int) error {
	return c.postID(id, "recheck", nil)
}

// hold posts pause or resume. The API's two verbs say the intent; the
// seam's Hold is the one door, and Pause stays nil so the desk does not
// draw a second key that would undo itself.
func (c *Client) hold(id int, on bool) error {
	name := "resume"
	if on {
		name = "pause"
	}
	return c.postID(id, name, nil)
}

func (c *Client) answer(id int, yes bool, words string) error {
	return c.postID(id, "answer", answerBody{Yes: yes, Words: words, Commit: c.seenJob(id).HeadCommit})
}

func (c *Client) steer(id int, words string) error {
	return c.postWords(context.Background(), id, "steer", words)
}

func (c *Client) sendBack(id int, words string) error {
	return c.postWords(context.Background(), id, "request_changes", words)
}

func (c *Client) say(ctx context.Context, id int, words string) error {
	return c.postWords(ctx, id, "reply", words)
}

// signOff posts approve. The wire answers with the approval's event id.
// Whether a habit is due is this machine's recipe, and this door does not
// carry it, so the bool stays false.
func (c *Client) signOff(id int, edited bool) (bool, error) {
	job := c.seenJob(id)
	if job.Publishing != nil && job.Publishing.State == "failed" {
		return false, c.postID(id, "retry_publish", nil)
	}
	err := c.postID(id, "approve", editedBody{Edited: edited, Commit: job.HeadCommit})
	return false, err
}

// mark posts prioritize. The verb only lifts a job to the top; taking a
// mark off sends an empty target and the server answers in its sentence.
func (c *Client) mark(ids []int, on bool) error {
	to := ""
	if on {
		to = "top"
	}
	for _, id := range ids {
		if err := c.postID(id, "prioritize", toBody{To: to}); err != nil {
			return err
		}
	}
	return nil
}

// make posts a new job. The server joins title and text with a blank line,
// so the split is that join undone and a title with no brief stays one string.
func (c *Client) make(repo, words string) (int, error) {
	title, text, _ := strings.Cut(words, "\n\n")
	var out struct {
		ID int `json:"id"`
	}
	err := c.call(context.Background(), http.MethodPost, "/v1/jobs", newJobBody{Repo: repo, Title: title, Text: text}, &out)
	if err != nil {
		return 0, err
	}
	return out.ID, nil
}

func (c *Client) setRepos(repos []string) error {
	// A nil list is a clear. The wire refuses null and accepts an empty array.
	if repos == nil {
		repos = []string{}
	}
	return c.call(context.Background(), http.MethodPut, "/v1/repos", watchedBody{Watched: repos}, nil)
}

func (c *Client) connect(ctx context.Context, token string) error {
	// An empty token is consent to use gh, so the field is sent either way.
	return c.call(ctx, http.MethodPost, "/v1/github/connect", tokenBody{Token: token}, nil)
}

func (c *Client) postID(id int, name string, body any) error {
	return c.call(context.Background(), http.MethodPost, jobPath(id, name), body, nil)
}

func (c *Client) postWords(ctx context.Context, id int, name, words string) error {
	return c.call(ctx, http.MethodPost, jobPath(id, name), wordsBody{Words: words}, nil)
}

func jobPath(id int, name string) string {
	return "/v1/jobs/" + strconv.Itoa(id) + "/" + name
}

type answerBody struct {
	Commit string `json:"commit,omitempty"`
	Yes    bool   `json:"yes"`
	Words  string `json:"words,omitempty"`
}

type wordsBody struct {
	Words string `json:"words"`
}

type editedBody struct {
	Commit string `json:"commit,omitempty"`
	Edited bool   `json:"edited"`
}

type toBody struct {
	To string `json:"to"`
}

type newJobBody struct {
	Repo  string `json:"repo"`
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
}

type watchedBody struct {
	Watched []string `json:"watched"`
}

type tokenBody struct {
	Token string `json:"token"`
}

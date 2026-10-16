package session

import (
	"context"
	"crypto/rand"
	"time"
)

type runIdentityKey struct{}

// withRunIdentity gives one execution an immutable identity. It never reads the
// agent's current turn after dispatch, so abandoned work cannot charge its successor.
func withRunIdentity(ctx context.Context) context.Context {
	return context.WithValue(ctx, runIdentityKey{}, rand.Text())
}
func runIdentity(ctx context.Context) string {
	id, _ := ctx.Value(runIdentityKey{}).(string)
	return id
}
func (s *sessionFile) appendRunIdentity(ctx context.Context) {
	if s == nil || runIdentity(ctx) == "" {
		return
	}
	s.writeLine(struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
	}{Type: "usage_run", ID: runIdentity(ctx), Timestamp: journalInstant(time.Now())})
}

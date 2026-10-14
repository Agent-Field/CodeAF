package space

import (
	"context"
	"reflect"
	"testing"
)

func TestMemberGone(t *testing.T) {
	tests := []struct {
		name   string
		hooks  []string
		member string
		want   []string
	}{
		{name: "no hooks", member: "raj"},
		{name: "nil hook", hooks: []string{""}, member: "raj"},
		{name: "registration order", hooks: []string{"first", "second"}, member: "raj", want: []string{"first:raj", "second:raj"}},
		{name: "nil between hooks", hooks: []string{"first", "", "second"}, member: "mona", want: []string{"first:mona", "second:mona"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memberGoneMu.Lock()
			previous := memberGoneHooks
			memberGoneHooks = nil
			memberGoneMu.Unlock()
			t.Cleanup(func() {
				memberGoneMu.Lock()
				memberGoneHooks = previous
				memberGoneMu.Unlock()
			})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var got []string
			for _, name := range tt.hooks {
				if name == "" {
					OnMemberGone(nil)
					continue
				}
				OnMemberGone(func(hookCtx context.Context, member string) {
					if hookCtx != ctx {
						t.Error("hook received a different context")
					}
					got = append(got, name+":"+member)
				})
			}
			memberGone(ctx, tt.member)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("hook calls = %v, want %v", got, tt.want)
			}
		})
	}
}

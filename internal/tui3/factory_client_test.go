package tui3

import (
	"flag"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/factorytest"
)

var factoryOverClient = flag.Bool("factory-client", false, "run factory page fixtures through the HTTP client and local desk doors")

// factoryTestSeam keeps local desk doors beside the client, as ordinary
// startup does. Missing fixture doors stay missing so capability tests still
// exercise the page's absence law rather than an HTTP refusal.
func factoryTestSeam(t testing.TB, local factory.Seam) factory.Seam {
	t.Helper()
	if !*factoryOverClient {
		return local
	}
	remote := factorytest.ServeInProcess(t, local)
	dst, src := reflect.ValueOf(&remote).Elem(), reflect.ValueOf(local)
	for i := 0; i < dst.NumField(); i++ {
		_, only := api.TUIOnly[dst.Type().Field(i).Name]
		if only || src.Field(i).IsNil() {
			dst.Field(i).Set(src.Field(i))
		}
	}
	return remote
}

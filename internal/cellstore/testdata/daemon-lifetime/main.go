// The lifetime fixture is a normal executable so the test observes production
// idle flags rather than the shorter lifetime of a Go test binary.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Agent-Field/codeaf/internal/cellstore"
)

type healthOp struct{}

func (healthOp) Verb() string   { return "health" }
func (healthOp) Args() []string { return nil }

func main() {
	app := flag.Bool("app", false, "use the desktop saving-helper lifetime")
	flag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *app {
		cellstore.SetAppDaemonLifetime(ctx)
	}
	d := cellstore.Daemon{Socket: os.Getenv("CODEAF_TEST_IDLE_SOCKET"), Binary: os.Getenv("CODEAF_TEST_IDLE_BINARY")}
	fmt.Println("ready")
	lines := bufio.NewScanner(os.Stdin)
	for lines.Scan() {
		switch lines.Text() {
		case "health":
			answer, err := d.Do(context.Background(), cellstore.Target{}, healthOp{})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			var health struct{ PID int }
			if err := json.Unmarshal(answer, &health); err != nil {
				panic(err)
			}
			fmt.Println(health.PID)
		case "quiet":
			cancel()
			fmt.Println("quiet")
		}
	}
}

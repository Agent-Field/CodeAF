package sessionhost

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// metadataAddresses are where cloud machines answer for their own credentials:
// the instance metadata address of AWS, Google Cloud and Azure, the task
// credentials address of AWS ECS, and AWS's IPv6 metadata address. Port 80 is
// one of the ports a chat's commands may connect to, and the fence filters by
// port and not by address, so a machine that answers here answers a chat too.
var metadataAddresses = []string{"169.254.169.254:80", "169.254.170.2:80", "[fd00:ec2::254]:80"}

// metadataWait is how long one address gets to answer. A network that does not
// route the address drops the packet, so the only way to say "no" is to stop waiting.
const metadataWait = time.Second

type dialer func(ctx context.Context, network, address string) (net.Conn, error)

// reachableMetadata names the addresses that accept a connection. It only
// connects: it sends nothing, so it asks the service for no credential, and it
// closes the connection at once.
func reachableMetadata(ctx context.Context, dial dialer) []string {
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		found = map[string]bool{}
	)
	for _, address := range metadataAddresses {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, metadataWait)
			defer cancel()
			conn, err := dial(ctx, "tcp", address)
			if err != nil {
				return
			}
			_ = conn.Close()
			mu.Lock()
			found[address] = true
			mu.Unlock()
		})
	}
	wg.Wait()
	var out []string
	for _, address := range metadataAddresses {
		if found[address] {
			out = append(out, address)
		}
	}
	return out
}

// WarnIfMetadataReachable checks once, in the background, whether this machine
// answers on a cloud metadata address, and says so on out when it does. It
// says nothing otherwise, and never stops the supervisor: the gap is the
// network's, and the operator closes it there (deploy/org/README.md, Known limits).
func WarnIfMetadataReachable(ctx context.Context, out io.Writer) {
	go warnIfMetadataReachable(ctx, out, (&net.Dialer{}).DialContext)
}

func warnIfMetadataReachable(ctx context.Context, out io.Writer, dial dialer) {
	if found := reachableMetadata(ctx, dial); len(found) > 0 {
		fmt.Fprintf(out, "sessionhost: WARNING: this machine answers on %v, a cloud metadata address, and a chat's commands can reach it, "+
			"because the network fence filters by port and not by address. Block 169.254.0.0/16 for this machine "+
			"(deploy/org/README.md, Known limits).\n", found)
	}
}

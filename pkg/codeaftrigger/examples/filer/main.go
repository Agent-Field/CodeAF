// Command filer files one ticket on a codeaf factory from standard input and
// prints the webhooks the factory sends back about it.
//
//	go run ./pkg/codeaftrigger/examples/filer -factory https://factory.example.com \
//	  -token cft_… -secret whsec_… -source ops -id T-1 -listen :8099 < ticket.txt
//
// The first line of standard input is the title; the rest is the body.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/pkg/codeaftrigger"
)

func main() {
	factory := flag.String("factory", "http://127.0.0.1:7420", "the factory's address")
	token := flag.String("token", "", "the source's token (codeaf factory source add prints it)")
	secret := flag.String("secret", "", "the source's signing secret")
	source := flag.String("source", "ops", "the registered source")
	id := flag.String("id", "T-1", "the ticket's id on your side")
	listen := flag.String("listen", ":8099", "where the factory's webhooks arrive (the source's --callback)")
	flag.Parse()
	in := bufio.NewReader(os.Stdin)
	title, _ := in.ReadString('\n')
	body, _ := io.ReadAll(in)
	client := codeaftrigger.New(*factory, *token)
	filed, err := client.File(context.Background(), codeaftrigger.Entry{
		Source: *source, ExternalID: *id, Title: strings.TrimSpace(title), Body: string(body)})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("filed job %d (new: %v); waiting for webhooks on %s\n", filed.ID, filed.Created, *listen)
	log.Fatal(http.ListenAndServe(*listen, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err := codeaftrigger.VerifyWebhook(*secret, raw, r.Header.Get(codeaftrigger.SignatureHeader)); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		ev, _ := codeaftrigger.ParseWebhook(raw)
		fmt.Printf("%s  job %d  %s %s\n", ev.Event, ev.Job, ev.Text, ev.URL)
	})))
}

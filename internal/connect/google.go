package connect

import (
	"context"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// The addresses Google answers on. They are variables rather than constants for
// one reason: a test points them at a server it started itself, so that every
// law in this package can be proved without a network and without an account.
// Nothing in the shipped program writes to them.
var (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	gmailBaseURL      = "https://gmail.googleapis.com/gmail/v1"
	googleCalendarURL = "https://www.googleapis.com/calendar/v3"
)

// google is the plug for a person's Google account, covering Gmail and
// Calendar behind one connection. Two services, one trip through the browser:
// nobody thinks of their mail and their calendar as two accounts to grant
// separately, and the permissions screen says plainly what is being asked for.
type google struct{}

func init() { Register(google{}) }

// googleScopes is the whole of what this plug asks a person for, in one place
// so that a screen, a stored connection and the request that goes out can never
// disagree about it.
//
// THE ASK IS THE SMALLEST ONE THAT DOES THE WORK, and each line is one:
//
//   - gmail.modify is read and write on the mailbox — searching, opening,
//     sending, drafts, labels — and it is one line rather than three because
//     Gmail's own documentation says so: modify supersedes readonly, and send
//     and compose are the halves of it a caller would otherwise have to ask for
//     separately. Three lines on a permissions screen that add up to what one
//     line says is a worse thing to read, not a safer one. What modify
//     deliberately stops short of is permanent deletion — a mailbox this
//     connection can write to is still one it cannot empty.
//   - calendar.events is read and write on the events of a person's calendars.
//     It is not calendar, which also carries the calendars themselves: making
//     and deleting whole calendars and changing who they are shared with is
//     nothing this program does, so it is nothing this program asks for.
var googleScopes = []string{
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/calendar.events",
}

func (google) Service() Service {
	return Service{
		ID:     "google",
		Name:   "Google",
		Blurb:  "Read and send Gmail; read and manage Calendar.",
		Auth:   AuthBrowser,
		Scopes: append([]string(nil), googleScopes...),
	}
}

func (google) Endpoint() oauth2.Endpoint {
	return oauth2.Endpoint{
		AuthURL:  googleAuthURL,
		TokenURL: googleTokenURL,
	}
}

// AuthCodeOptions asks Google for the two things it will not give by default.
//
// GOOGLE ISSUES A REFRESH KEY ONLY ON A FRESH GRANT. Offline access alone is
// not enough: a person who has approved aforge before is bounced straight back
// with an access key that expires in an hour and nothing to renew it with,
// which looks exactly like a successful connection until it silently stops
// working. Forcing the consent screen every time costs one extra click and
// makes the connection durable.
func (google) AuthCodeOptions() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	}
}

// Account asks Gmail whose mailbox this is. The answer is the address a person
// recognises, which is the only identity worth showing next to "Connected".
//
// Failure here is ordinary — a slow network, a service having a bad minute —
// and the caller is expected to keep the connection and store no account at
// all rather than fail a connection that otherwise worked.
func (google) Account(ctx context.Context, client *http.Client) (string, error) {
	var profile struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := getJSON(ctx, client, gmailBaseURL+"/users/me/profile", &profile); err != nil {
		return "", err
	}
	return strings.TrimSpace(profile.EmailAddress), nil
}

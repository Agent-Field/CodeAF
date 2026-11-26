package connect

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/oauth2"
)

const Microsoft365 = "microsoft-365"
const MicrosoftGraph = "https://graph.microsoft.com/v1.0"
const MicrosoftTranscripts = "OnlineMeetingTranscript.Read.All"

var ErrMicrosoftRegistration = errors.New("Microsoft 365 needs an application ID and either common or a directory tenant ID")

var entraID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var entraCode = regexp.MustCompile(`\bAADSTS([0-9]+)\b`)

type microsoft struct {
	tenant, app string
	transcripts bool
}

func microsoftRegistration(c ClientCredential) (microsoft, error) {
	tenant := strings.ToLower(strings.TrimSpace(c.Tenant))
	if tenant == "" {
		tenant = "common"
	}
	id := strings.ToLower(strings.TrimSpace(c.ID))
	if !entraID.MatchString(id) || tenant != "common" && !entraID.MatchString(tenant) {
		return microsoft{}, ErrMicrosoftRegistration
	}
	return microsoft{tenant: tenant, app: id, transcripts: c.Transcripts}, nil
}

func (p microsoft) Service() Service {
	scopes := []string{"User.Read", "User.ReadBasic.All", "Mail.ReadWrite", "Calendars.ReadWrite", "Files.Read.All"}
	if p.transcripts {
		scopes = append(scopes, "OnlineMeetings.Read", MicrosoftTranscripts)
	}
	return Service{ID: Microsoft365, Name: "Microsoft 365", Category: categoryProductivity,
		Blurb: "Connect Outlook, Calendar, and files to apps.", Auth: AuthBrowser, Scopes: scopes}
}

func (p microsoft) Endpoint() oauth2.Endpoint {
	base := "https://login.microsoftonline.com/" + p.tenant + "/oauth2/v2.0"
	return oauth2.Endpoint{AuthURL: base + "/authorize", TokenURL: base + "/token", AuthStyle: oauth2.AuthStyleInParams}
}

func (p microsoft) AuthCodeOptions() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{oauth2.SetAuthURLParam("scope", strings.Join(p.Service().Scopes, " ")+" offline_access")}
}

func (p microsoft) Account(ctx context.Context, client *http.Client) (string, error) {
	var user struct {
		ID                string
		Mail              string
		UserPrincipalName string
	}
	// An OAuth transport can add Authorization again on redirects. Keep the
	// identity probe on Graph even if an upstream server returns a redirect.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	err := getJSON(ctx, client, MicrosoftGraph+"/me?$select=id,mail,userPrincipalName", &user)
	if err != nil || user.ID == "" {
		return "", errors.New("Microsoft 365 could not confirm the signed-in account")
	}
	if user.Mail != "" {
		return user.Mail, nil
	}
	return user.UserPrincipalName, nil
}

func (p microsoft) registrationID() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(p.tenant+"/"+p.app)))
}

type entraFailure string

func (e entraFailure) Error() string { return string(e) }

func microsoftSignInError(err error) error {
	if err == nil {
		return nil
	}
	var vendor *oauth2.RetrieveError
	description := ""
	if errors.As(err, &vendor) {
		description = vendor.ErrorDescription
	} else if _, raw, ok := strings.Cut(err.Error(), "authorization error from server:"); ok {
		description = raw
	}
	match := entraCode.FindStringSubmatch(description)
	if len(match) == 2 {
		switch match[1] {
		case "65004", "90094", "900941", "900981":
			return entraFailure("Your Microsoft administrator must approve this connection.")
		case "65001":
			return entraFailure("Microsoft consent is missing. Your organization may require an administrator to approve this connection.")
		case "53000", "53001", "53002", "53003", "53004":
			return entraFailure("Your organization's Microsoft Conditional Access policy blocked this connection.")
		case "50076", "50079":
			return entraFailure("Microsoft requires another authentication step. Start the connection again and complete the prompt.")
		}
	}
	return entraFailure("Microsoft 365 sign-in did not finish. Start the connection again.")
}

// MicrosoftGrant identifies the exact authorization independently of refreshes.
// Callers use it to fence in-flight requests, cursors, and approved effects.
func (m *Manager) MicrosoftGrant() (string, bool, error) {
	p, err := m.plug(Microsoft365)
	if err != nil {
		return "", false, errors.New("Set up a Microsoft 365 app registration before connecting.")
	}
	entry, ok := m.standing(p)
	if !ok {
		return "", false, errors.New("Connect your Microsoft 365 account before using this source.")
	}
	return entry.GrantID, entry.covers([]string{MicrosoftTranscripts, "OnlineMeetings.Read"}), nil
}

// MicrosoftRequestError uses the same refusal wording for token refresh.
func MicrosoftRequestError(err error) error {
	var response *oauth2.RetrieveError
	if errors.As(err, &response) {
		return microsoftSignInError(err)
	}
	return nil
}

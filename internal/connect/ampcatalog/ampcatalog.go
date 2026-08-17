// Package ampcatalog is aforge's own copy of the parts of the amp-labs
// provider catalog that this program reads.
//
// WHY A COPY RATHER THAN THE LIBRARY. The catalog aforge wants is data: a few
// hundred rows of display name, base URL, auth type, and where a key rides on
// a request. The library that publishes it is a full connector SDK, and
// importing the one package that holds those rows drags in its whole world —
// the AWS SDK, OpenTelemetry, an HTML parser, x/text's CJK encodings, a struct
// validator, and (via a test helper left in the import graph) the testing
// package itself. Measured on its own that import is 9.6 MB of a 44 MB binary,
// for a table.
//
// So the table is taken at build time and kept here. `go generate ./...` runs
// gen/main.go, which imports the real catalog and writes providers.json beside
// this file; the library stays a module requirement and stops being a linked
// dependency. A catalog refresh is `go generate ./internal/connect/ampcatalog`
// and reading the diff — and the diff is readable, which is the second reason
// the snapshot is JSON rather than a Go literal.
//
// THE SHAPE IS THE LIBRARY'S, DELIBERATELY. The type names, field names and
// constant names below are the ones amp uses, spelled the same way, so the
// call sites read identically against either and so a future field is a
// two-line change in gen/main.go rather than a translation layer. Only the
// fields aforge actually reads are carried; a field nobody reads is a field
// nobody can be wrong about.
package ampcatalog

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sync"
)

//go:generate go run ./gen

//go:embed providers.json
var snapshot []byte

// Provider is a catalog key — the service's id, as amp spells it.
type Provider string

// AuthType is how a service expects to be authenticated.
//
// All six the catalog uses are named, not only the two aforge can drive
// itself: the switches that read this fall through the other four, and a
// falling-through case a reader can name is easier to be sure about than a
// bare string.
type AuthType string

const (
	ApiKey AuthType = "apiKey"
	Basic  AuthType = "basic"
	Custom AuthType = "custom"
	Jwt    AuthType = "jwt"
	None   AuthType = "none"
	Oauth2 AuthType = "oauth2"
)

// ApiKeyOptsAttachmentType is where a key rides on a request.
type ApiKeyOptsAttachmentType string

const (
	Header ApiKeyOptsAttachmentType = "header"
	Query  ApiKeyOptsAttachmentType = "query"
)

// ProviderInfo is one catalog row, cut to what aforge reads.
type ProviderInfo struct {
	DisplayName     string            `json:"displayName,omitempty"`
	BaseURL         string            `json:"baseURL"`
	AuthType        AuthType          `json:"authType"`
	ApiKeyOpts      *ApiKeyOpts       `json:"apiKeyOpts,omitempty"`
	BasicOpts       *BasicAuthOpts    `json:"basicOpts,omitempty"`
	Metadata        *ProviderMetadata `json:"metadata,omitempty"`
	AuthHealthCheck *AuthHealthCheck  `json:"authHealthCheck,omitempty"`
}

// ApiKeyOpts says where the key goes and under what name.
type ApiKeyOpts struct {
	AttachmentType ApiKeyOptsAttachmentType `json:"attachmentType"`
	DocsURL        string                   `json:"docsURL,omitempty"`
	Header         *ApiKeyOptsHeader        `json:"header,omitempty"`
	Query          *ApiKeyOptsQuery         `json:"query,omitempty"`
}

// ApiKeyOptsHeader is a key carried in a request header.
type ApiKeyOptsHeader struct {
	Name        string `json:"name"`
	ValuePrefix string `json:"valuePrefix,omitempty"`
}

// ApiKeyOptsQuery is a key carried in the query string.
type ApiKeyOptsQuery struct {
	Name string `json:"name"`
}

// BasicAuthOpts covers the services that collect an API key through basic auth.
type BasicAuthOpts struct {
	DocsURL           string             `json:"docsURL,omitempty"`
	ApiKeyAsBasicOpts *ApiKeyAsBasicOpts `json:"apiKeyAsBasicOpts,omitempty"`
}

// ApiKeyAsBasicOpts is how that key has to be spelled before it is encoded.
type ApiKeyAsBasicOpts struct {
	KeyFormat string `json:"keyFormat,omitempty"`
}

// ProviderMetadata is what a service needs asking about before it can be
// addressed — the workspace in https://<workspace>.example.com.
type ProviderMetadata struct {
	Input []MetadataItemInput `json:"input,omitempty"`
}

// MetadataItemInput is one such question.
type MetadataItemInput struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

// AuthHealthCheck is the cheap authenticated call that proves a key works.
type AuthHealthCheck struct {
	Url                string `json:"url"`
	Method             string `json:"method,omitempty"`
	SuccessStatusCodes []int  `json:"successStatusCodes,omitempty"`
}

// ErrProviderNotFound is returned for a name the snapshot does not carry. It
// mirrors the library's own error for the same case, because the one caller
// treats any error as "skip this row".
var ErrProviderNotFound = errors.New("provider not found")

// catalog decodes the snapshot on first use rather than at init.
//
// Nothing on the launch path asks the catalog anything: the rows are read when
// the accounts surface opens, which is a keystroke away at the earliest and
// never happens in most runs. Decoding here would put the whole table on every
// `aforge` invocation, `--help` included, for nothing.
var catalog = sync.OnceValue(func() map[Provider]*ProviderInfo {
	var rows map[Provider]*ProviderInfo
	if err := json.Unmarshal(snapshot, &rows); err != nil {
		// The snapshot is generated, committed, and read by this package's own
		// tests. A parse failure here is a broken build, not a runtime mode.
		panic("ampcatalog: snapshot is not valid JSON: " + err.Error())
	}
	return rows
})

// AllNames returns every provider the snapshot carries, in no particular
// order. The one caller sorts, which is what makes two builds register the
// same list in the same order.
func AllNames() []Provider {
	rows := catalog()
	out := make([]Provider, 0, len(rows))
	for name := range rows {
		out = append(out, name)
	}
	return out
}

// ReadInfo returns one row. The value is shared, not copied: every caller in
// this program reads it and none writes to it, which is also true of the
// library this replaces.
func ReadInfo(provider Provider) (*ProviderInfo, error) {
	if info, ok := catalog()[provider]; ok {
		return info, nil
	}
	return nil, ErrProviderNotFound
}

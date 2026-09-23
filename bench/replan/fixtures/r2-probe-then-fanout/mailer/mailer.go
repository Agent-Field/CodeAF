// Package mailer fetches the templates outgoing mail is written from.
package mailer

import "bloop/shop/legacy"

// DefaultTemplate is used for any template the service does not have.
const DefaultTemplate = "Hello {{name}}"

// Template answers the named template. The template service is asked once:
// a slow template is worse than the default one.
func Template(name string) (string, error) {
	body, err := legacy.Fetch("https://templates.local/"+name, 0)
	if err != nil {
		return "", err
	}
	if body == "" {
		return DefaultTemplate, nil
	}
	return body, nil
}

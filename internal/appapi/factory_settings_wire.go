package appapi

import "time"

// FactorySettingsDetails is the Settings view of the selected, attached factory.
type FactorySettingsDetails struct {
	ID                   string                  `json:"id"`
	Mode                 string                  `json:"mode" jsonschema:"enum=solo,enum=team"`
	Host                 string                  `json:"host"`
	SignInLink           string                  `json:"sign_in_link,omitempty"`
	Login                string                  `json:"login"`
	SignedIn             bool                    `json:"signed_in"`
	Attached             bool                    `json:"attached"`
	Operator             bool                    `json:"operator"`
	Repos                []FactorySettingsRepo   `json:"repos"`
	Links                []FactorySettingsLink   `json:"links"`
	People               []FactorySettingsPerson `json:"people"`
	RunnerTimeoutSeconds int                     `json:"runner_timeout_s"`
	KeepOutputDays       int                     `json:"keep_output_days"`
	KeepOutputMB         int                     `json:"keep_output_mb"`
}
type FactorySettingsRepo struct {
	ID      string   `json:"id"`
	Names   []string `json:"names"`
	Enabled bool     `json:"enabled"`
}
type FactorySettingsLink struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}
type FactorySettingsPerson struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Initial string `json:"initial"`
	Hue     int    `json:"hue"`
}
type PatchFactorySettingsRequest struct {
	RunnerTimeoutSeconds *int `json:"runner_timeout_s,omitempty"`
	KeepOutputDays       *int `json:"keep_output_days,omitempty"`
	KeepOutputMB         *int `json:"keep_output_mb,omitempty"`
}
type FactoryRepoRequest struct {
	Enabled *bool `json:"enabled"`
}
type FactoryLinkRequest struct {
	Login     string    `json:"login,omitempty"`
	Name      string    `json:"name,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}
type FactoryAdvancedRequest struct {
	RunnerTimeoutSeconds int `json:"runner_timeout_s"`
}
type FactoryRunnerRequest struct {
	KeepOutputDays *int `json:"keep_output_days,omitempty"`
	KeepOutputMB   *int `json:"keep_output_mb,omitempty"`
}
type FactoryLinkResponse struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	ID     string `json:"id"`
	URL    string `json:"url"`
}
type DeleteFactoryLinkRequest struct {
	ID string `json:"id"`
}

package config

import (
	"os"
	"testing"
)

// TestMain clears the Azure OpenAI variables once for the package. Unlike a
// vendored service's key, which a test clears by name when it counts
// services, these two ADD a service by being set, so a developer machine with
// them exported would grow every resolved set by one and fail tests that have
// nothing to do with Azure. A test that wants the service sets them itself.
func TestMain(m *testing.M) {
	os.Unsetenv(AzureBaseEnv)
	os.Unsetenv("AZURE_API_KEY")
	os.Exit(m.Run())
}

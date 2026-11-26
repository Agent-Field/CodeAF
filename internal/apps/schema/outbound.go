package schema

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/extensions"
)

// ToolApprovalKind reads the operation's approval from the current manifest.
// Older and external tools retain the compatibility rule for outbound verbs.
func ToolApprovalKind(tool string) string {
	tool = strings.ToLower(strings.TrimSpace(tool))
	if _, ok := extensions.ToolID(tool); ok {
		return ""
	}
	service, operation, _ := strings.Cut(tool, ".")
	if manifest, ok := schemas().kits[2].Registries.Tools[service].Operations[operation]; ok {
		return approvalKind(manifest)
	}
	if isOutbound(tool) {
		return "send"
	}
	return ""
}

// IsOutboundTool reports whether the operation needs an approval.
func IsOutboundTool(tool string) bool {
	return ToolApprovalKind(tool) != ""
}

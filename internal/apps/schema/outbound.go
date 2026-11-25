package schema

import "strings"

// ToolApprovalKind reads the operation's approval from the current manifest.
// Older and external tools retain the compatibility rule for outbound verbs.
func ToolApprovalKind(tool string) string {
	tool = strings.ToLower(strings.TrimSpace(tool))
	service, operation, _ := strings.Cut(tool, ".")
	if manifest, ok := schemas().kits[2].Registries.Tools[service].Operations[operation]; ok {
		return approvalKind(manifest)
	}
	if isOutbound(tool) {
		return "send"
	}
	return ""
}

func IsOutboundTool(tool string) bool {
	return ToolApprovalKind(tool) != ""
}

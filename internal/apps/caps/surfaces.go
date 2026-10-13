package caps

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Surface calls use app roles; they do not inherit record-type capabilities.
var surfaceRules = map[string]bool{
	bridge.MethodDocsList:        false,
	bridge.MethodDocsGet:         false,
	bridge.MethodDocsCreate:      true,
	bridge.MethodDocsReorder:     true,
	bridge.MethodPartsGet:        false,
	bridge.MethodPartsLock:       true,
	bridge.MethodPartsTakeover:   true,
	bridge.MethodPartsAnswer:     true,
	bridge.MethodPartsRelease:    true,
	bridge.MethodPartsWrite:      true,
	bridge.MethodPartsAdd:        true,
	bridge.MethodPartsRemove:     true,
	bridge.MethodPartsVersions:   false,
	bridge.MethodPartsDiff:       false,
	bridge.MethodPartsRestore:    true,
	bridge.MethodPresenceSet:     false,
	bridge.MethodCommentsList:    false,
	bridge.MethodCommentsAdd:     true,
	bridge.MethodCommentsResolve: true,
	bridge.MethodCommentsSend:    true,
	bridge.MethodShareSet:        true,
	bridge.MethodShareOpen:       false,
}

func allowSurfaces(scope svc.Scope, method string, params json.RawMessage) (bool, error) {
	write, known := surfaceRules[method]
	if !known && !surfaceTopic(method, params) {
		return false, nil
	}
	switch scope.Caller.Role {
	case svc.RoleEditor, svc.RoleUser:
	case svc.RoleViewer:
		if write {
			return true, denied("the app role does not allow this call")
		}
	default:
		return true, denied("the app role does not allow this call")
	}
	if len(params) != 0 && string(params) != "null" && (!json.Valid(params) || !strings.HasPrefix(strings.TrimSpace(string(params)), "{")) {
		return true, bridge.Errorf(bridge.CodeInvalid, "params must be an object")
	}
	if scope.App == "" {
		return true, bridge.Errorf(bridge.CodeInvalid, "this method needs app")
	}
	if known {
		return true, nil
	}
	return true, allowDocTopic(scope, params)
}

func surfaceTopic(method string, params json.RawMessage) bool {
	if method != "sub" && method != "unsub" {
		return false
	}
	var p struct {
		Topic string `json:"topic"`
	}
	if json.Unmarshal(params, &p) != nil {
		return false
	}
	segments := strings.SplitN(p.Topic, "/", 4)
	return len(segments) >= 3 && segments[0] == "app" && segments[2] == "doc"
}

func allowDocTopic(scope svc.Scope, params json.RawMessage) error {
	var p struct {
		Topic string `json:"topic"`
	}
	if json.Unmarshal(params, &p) != nil {
		return bridge.Errorf(bridge.CodeInvalid, "topic must be a string")
	}
	doc, ok := strings.CutPrefix(p.Topic, bridge.TopicDoc(scope.App, ""))
	if !ok || doc == "" || path.Clean(doc) != doc || strings.HasPrefix(doc, "/") || strings.ContainsAny(doc, "*\\") {
		return bridge.Errorf(bridge.CodeInvalid, "topic must name a document in this app")
	}
	for _, segment := range strings.Split(doc, "/") {
		if segment == "." || segment == ".." {
			return bridge.Errorf(bridge.CodeInvalid, "topic must name a document in this app")
		}
	}
	return nil
}

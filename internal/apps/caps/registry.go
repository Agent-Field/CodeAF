package caps

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// AllowFunc decides permission for exactly one registered method or topic prefix.
type AllowFunc func(ctx context.Context, caller svc.Caller, scope svc.Scope, params json.RawMessage) error

// DenyRule adds a limit without granting permission to any method.
type DenyRule func(ctx context.Context, caller svc.Caller, scope svc.Scope, method string, params json.RawMessage) error

type topicAllow struct {
	prefix string
	allow  AllowFunc
}

var permissions = struct {
	sync.RWMutex
	methods map[string]AllowFunc
	topics  []topicAllow
	denies  []DenyRule
}{methods: make(map[string]AllowFunc)}

// RegisterAllow reserves even nil entries so another track cannot replace wave-1 permissions.
// Gateway forwards every method registration here to avoid an import cycle.
func RegisterAllow(name string, allow AllowFunc) {
	permissions.Lock()
	defer permissions.Unlock()
	if _, exists := permissions.methods[name]; exists {
		panic("caps: method " + name + " registered twice")
	}
	permissions.methods[name] = allow
}

// RegisterTopic accepts a literal prefix with an optional wildcard app id, such as app/*/shape/.
// Overlapping prefixes are refused so a track cannot intercept another track's permissions.
func RegisterTopic(prefix string, allow AllowFunc) {
	permissions.Lock()
	defer permissions.Unlock()
	for _, topic := range permissions.topics {
		if topicPrefixMatches(topic.prefix, prefix) || topicPrefixMatches(prefix, topic.prefix) {
			panic("caps: topic prefix " + prefix + " registered twice or overlaps")
		}
	}
	permissions.topics = append(permissions.topics, topicAllow{prefix: prefix, allow: allow})
}

// AddDenyRule preserves registration order; returning nil only continues the normal check.
func AddDenyRule(rule DenyRule) {
	permissions.Lock()
	defer permissions.Unlock()
	permissions.denies = append(permissions.denies, rule)
}

func registeredPermission(method string, params json.RawMessage) (AllowFunc, []DenyRule) {
	permissions.RLock()
	defer permissions.RUnlock()
	allow := permissions.methods[method]
	if method == "sub" || method == "unsub" {
		var p struct {
			Topic string `json:"topic"`
		}
		if json.Unmarshal(object(params), &p) == nil {
			for _, topic := range permissions.topics {
				if topicPrefixMatches(topic.prefix, p.Topic) {
					allow = topic.allow
					break
				}
			}
		}
	}
	return allow, append([]DenyRule(nil), permissions.denies...)
}

func topicPrefixMatches(prefix, topic string) bool {
	if suffix, ok := strings.CutPrefix(prefix, "app/*/"); ok {
		parts := strings.SplitN(topic, "/", 3)
		return len(parts) == 3 && parts[0] == "app" && parts[1] != "" && strings.HasPrefix(parts[2], suffix)
	}
	return strings.HasPrefix(topic, prefix)
}

package hub

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestCodexStartPermissionTransportAndOldNode(t *testing.T) {
	h := newTestHub(t)
	h.store = newMemStore()
	var seen []map[string]any
	old := false
	modelsNode(t, h, MachineConfig{ID: "box"}, func(method string, p map[string]any) (any, error) {
		if method == "session.start_codex" {
			seen = append(seen, p)
			if old {
				return nil, &rpc.Error{Message: "unknown method session.start_codex"}
			}
			return map[string]any{"session": proto.Session{SessionID: "session-1", Agent: "codex"}}, nil
		}
		if method == "session.start" {
			t.Error("Codex choice used the legacy start RPC")
		}
		return map[string]any{}, nil
	})
	connectAssistant(t, h)
	for _, choice := range []string{"", "full-access"} {
		body, _ := json.Marshal(map[string]any{"machine_id": "box", "cwd": "/workspace", "launcher": "codex", "codex_permissions": choice})
		w := modelsRequest(h, "POST", "/api/v1/sessions/codex", string(body))
		if w.Code != http.StatusOK {
			t.Fatalf("start %q: %d %s", choice, w.Code, w.Body)
		}
		got := seen[len(seen)-1]
		if value, present := got["codex_permissions"]; !present || value != choice {
			t.Fatalf("choice lost: %v", got)
		}
		if got["permission_mode"] != nil && got["permission_mode"] != "" || got["effort"] != nil && got["effort"] != "" {
			t.Fatalf("Codex altered Claude preferences: %v", got)
		}
	}
	old = true
	w := modelsRequest(h, "POST", "/api/v1/sessions/codex", `{"machine_id":"box","cwd":"/workspace","launcher":"codex","codex_permissions":"read-only"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("old node: %d %s", w.Code, w.Body)
	}
	var refusal map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if refusal["code"] != proto.CodeNodeOutdated {
		t.Fatalf("old node lost capability refusal: %s", w.Body)
	}
}

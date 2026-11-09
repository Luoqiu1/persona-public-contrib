package hub

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/rpc"
)

func TestCodexPermissionRoutesPreservePoliciesAndLocalizeOldNodes(t *testing.T) {
	h := newTestHub(t)
	var calls []string
	modelsNode(t, h, MachineConfig{ID: "box"}, func(method string, params map[string]any) (any, error) {
		b, _ := json.Marshal(params)
		calls = append(calls, method+" "+string(b))
		if method == "session.codex_permissions" {
			return nil, &rpc.Error{Code: "unknown", Message: "unknown method session.codex_permissions"}
		}
		return map[string]any{}, nil
	})
	for _, method := range []string{"GET", "POST"} {
		body := ""
		if method == "POST" {
			body = `{"choice":"full-access"}`
		}
		w := modelsRequest(h, method, "/api/v1/sessions/box/s1/codex-permissions", body)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "node_outdated") || !strings.Contains(w.Body.String(), "Codex") {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
	}
	if len(calls) != 2 || !strings.Contains(calls[1], `"codex_permissions":"full-access"`) || strings.Contains(strings.Join(calls, "\n"), "session.modes") {
		t.Fatalf("wrong route: %v", calls)
	}
}

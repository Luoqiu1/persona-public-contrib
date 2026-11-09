package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A distinct RPC prevents old nodes from ignoring a permission field and returning
// success. Keep refusals localized here; node error codes are the stable boundary.
func codexPermissionNodeError(err error) error {
	var re *rpc.Error
	if strings.HasPrefix(err.Error(), "unknown method") || errors.As(err, &re) && (strings.HasPrefix(re.Message, "unknown method") || re.Code == "method_not_found") {
		return &rpc.Error{Code: proto.CodeNodeOutdated, Message: "Codex permission controls require a newer node"}
	}
	return err
}

func codexPermissionError(w http.ResponseWriter, err error) {
	err = codexPermissionNodeError(err)
	var re *rpc.Error
	if errors.As(err, &re) {
		code := re.Code
		if code == proto.CodeNodeOutdated || strings.HasPrefix(code, "codex_permission") || code == "codex_launch_preferences" || code == "identity_conflict" {
			writeErrCode(w, http.StatusConflict, code, i18n.Err("hub-codex-permissions."+code))
			return
		}
	}
	callErr(w, err)
}

func (h *Hub) codexPermissionsRead() http.HandlerFunc {
	return h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		var out json.RawMessage
		if err := m.call(r.Context(), "session.codex_permissions", map[string]any{"session_id": r.PathValue("sid")}, &out); err != nil {
			codexPermissionError(w, err)
			return
		}
		writeJSON(w, out)
	})
}
func (h *Hub) codexPermissionsWrite() http.HandlerFunc {
	return h.withMachine(func(w http.ResponseWriter, r *http.Request, m *Machine) {
		var p struct {
			Choice *string `json:"choice"`
		}
		if err := decodeJSONBody(w, r, &p, 4096); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		if p.Choice == nil || *p.Choice == "" {
			writeErr(w, http.StatusBadRequest, i18n.Err("hub-codex-permissions.codex_permissions_invalid"))
			return
		}
		var out json.RawMessage
		if err := m.call(r.Context(), "session.codex_permissions", map[string]any{"session_id": r.PathValue("sid"), "codex_permissions": *p.Choice}, &out); err != nil {
			codexPermissionError(w, err)
			return
		}
		writeJSON(w, out)
	})
}

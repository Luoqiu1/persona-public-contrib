package hub

import (
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
		if code == proto.CodeNodeOutdated || strings.HasPrefix(code, "codex_permission") || code == "codex_launch_preferences" {
			writeErrCode(w, http.StatusConflict, code, i18n.Err("hub-codex-permissions."+code))
			return
		}
	}
	callErr(w, err)
}

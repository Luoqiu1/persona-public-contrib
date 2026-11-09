package node

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/proto"
)

// Codex permissions are independent of Claude's permission modes. Empty means follow
// the selected launcher (and, for the stock Orca launcher, Orca's launch profile).
func codexPermissionFlags(choice string) ([]string, error) {
	switch choice {
	case "":
		return nil, nil
	case "read-only", "workspace-write":
		flags := []string{"-a", "on-request", "-s", choice, "-c", "approvals_reviewer=user"}
		if choice == "workspace-write" {
			flags = append(flags, "-c", "sandbox_workspace_write.network_access=false", "-c", "sandbox_workspace_write.writable_roots=[]")
		}
		return flags, nil
	case "full-access":
		return []string{"--dangerously-bypass-approvals-and-sandbox"}, nil
	default:
		return nil, refusal("codex_permissions_invalid", "Invalid Codex permission choice", nil)
	}
}

// A profile or explicit permission carrier belongs to the launcher. Refuse a conflicting
// session override rather than guessing at TOML/profile precedence or dropping arguments.
func codexPermissionConfig(value string) bool {
	key, _, ok := strings.Cut(value, "=")
	if !ok {
		return false
	}
	root := strings.Trim(strings.TrimSpace(strings.SplitN(key, ".", 2)[0]), "\"'")
	return slices.Contains([]string{"approval_policy", "sandbox_mode", "sandbox_workspace_write", "approvals_reviewer", "permissions"}, root)
}

func codexPermissionCarrier(argv []string) bool {
	for i, word := range argv {
		name := strings.SplitN(word, "=", 2)[0]
		switch name {
		case "-a", "--ask-for-approval", "-s", "--sandbox", "-p", "--profile", "--permissions", "--full-auto", "--approve-for-me", "--yolo", "--not-so-yolo", "--dangerously-bypass-approvals-and-sandbox":
			return true
		case "-c", "--config":
			value := ""
			if _, v, ok := strings.Cut(word, "="); ok {
				value = v
			} else if i+1 < len(argv) {
				value = argv[i+1]
			}
			if codexPermissionConfig(value) {
				return true
			}
		}
		if len(word) > 2 && !strings.HasPrefix(word, "--") {
			if slices.Contains([]string{"-a", "-s", "-p"}, word[:2]) {
				return true
			}
			if strings.HasPrefix(word, "-c") && codexPermissionConfig(word[2:]) {
				return true
			}
		}
	}
	return false
}

func (n *Node) codexLaunchPermissions(ctx context.Context, launch host.Launcher, launcher string, argv []string, choice *string) ([]string, error) {
	// Copy the cached launcher's slice: one session must never mutate another launch.
	argv = slices.Clone(argv)
	selected := ""
	if choice != nil {
		selected = *choice
	}
	flags, err := codexPermissionFlags(selected)
	if err != nil {
		return nil, err
	}
	if len(flags) > 0 {
		check := argv
		if managed, ok := n.managedLauncher(launcher); ok {
			check = append(slices.Clone(check), managed.Args...)
		}
		if codexOnRemote(check) || codexPermissionCarrier(check) {
			return nil, refusal("codex_permission_conflict", "The launcher already selects a Codex permission policy or profile. Follow the launcher or choose a launcher without permission overrides.", nil)
		}
		return append(argv, flags...), nil
	}
	// Only the stock launcher follows Orca's stock launch profile. Aliases and managed
	// launchers retain their own complete configuration, including environment/profile flags.
	if h, ok := launch.(*orcaHost); ok && launcher == "codex" && len(argv) == 1 && argv[0] == "codex" {
		var res struct {
			Settings struct {
				Commands map[string]string   `json:"agentCmdOverrides"`
				Args     map[string]string   `json:"agentDefaultArgs"`
				Env      map[string][]string `json:"environmentNames"`
			} `json:"settings"`
		}
		if err := h.c.Call(ctx, orcart.CallTimeout, "settings.getAgentLaunch", nil, &res); err != nil {
			// No fallback: on an old/unavailable host we cannot claim to have followed its preferences.
			return nil, refusal("codex_launch_preferences", "Could not read Orca's launch preferences. Update Orca or select an explicitly configured launcher.", nil)
		}
		command := res.Settings.Commands["codex"]
		if command != "" && command != "codex" || len(res.Settings.Env["codex"]) > 0 {
			return nil, refusal("codex_launch_preferences", "Orca's Codex launcher has a custom command or environment. Select that configured launcher in Persona.", nil)
		}
		rawArgs := res.Settings.Args["codex"]
		if strings.ContainsAny(rawArgs, "\n\r") {
			return nil, refusal("codex_launch_preferences", "Orca launch arguments require a shell", nil)
		}
		args := shellWords(rawArgs)
		// Exec literal argv, never evaluate shell operators, substitutions or variables.
		for _, arg := range args {
			if strings.ContainsAny(arg, "$`<>\x00") || slices.Contains([]string{";", "&", "&&", "|", "||", ">", "<"}, arg) {
				return nil, refusal("codex_launch_preferences", "Orca's launch arguments require a shell. Select an explicitly configured launcher.", nil)
			}
		}
		argv = append(argv, args...)
	}
	return argv, nil
}

// codexPermissions reads the server's effective thread settings using a resume with
// no overrides. It never types into a terminal, changes global config or queues a policy
// for a later turn. A missing method/readback remains unsupported, never a successful write.
func (n *Node) codexPermissions(ctx context.Context, id string, choice *string) (*proto.CodexPermissions, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if choice != nil {
		if _, err := codexPermissionFlags(*choice); err != nil || *choice == "" {
			return nil, refusal("codex_permissions_invalid", "Invalid Codex permission choice", nil)
		}
	}
	out := &proto.CodexPermissions{Reason: "terminal", Choices: []string{}}
	if n.isAssistantSession(id) {
		return nil, refusal("codex_permissions_unsupported", "Codex Agent permissions do not apply to assistant conversations", nil)
	}
	c, t, err := n.codexOwner(id)
	if err != nil || t.parent != "" || !t.joined {
		if choice != nil {
			return nil, refusal("codex_permissions_unsupported", "This session has no supported Codex permission connection. Change permissions in its original app or terminal.", nil)
		}
		if m := n.metaOf(id); n.codexPath(id) != "" {
			out.ApprovalPolicy, out.Sandbox = m.mode, m.sandbox
		}
		return out, nil
	}
	defer c.joins.lock("permissions:" + t.id)()
	// Refresh the owner after acquiring the write lock; reconnects must not redirect writes.
	owner, current, err := n.codexOwner(id)
	if err != nil || owner != c || current.id != t.id {
		return nil, refusal("codex_permissions_unavailable", "Codex connection changed; refresh and try again", nil)
	}
	read := func() error {
		loaded, err := c.loaded(ctx)
		if err != nil || !slices.Contains(loaded, t.id) {
			return errCodexNoOwner
		}
		var res struct {
			Thread   remoteThreadData `json:"thread"`
			Approval json.RawMessage  `json:"approvalPolicy"`
			Reviewer string           `json:"approvalsReviewer"`
			Sandbox  json.RawMessage  `json:"sandbox"`
		}
		if err := c.wire.call(ctx, "thread/resume", map[string]any{"threadId": t.id, "excludeTurns": true}, &res); err != nil {
			return err
		}
		var approval string
		_ = json.Unmarshal(res.Approval, &approval)
		var sandbox struct {
			Type    string          `json:"type"`
			Network json.RawMessage `json:"networkAccess"`
		}
		_ = json.Unmarshal(res.Sandbox, &sandbox)
		var network bool
		_ = json.Unmarshal(sandbox.Network, &network)
		if string(sandbox.Network) == `"enabled"` || sandbox.Type == "dangerFullAccess" {
			network = true
		}
		out.ApprovalPolicy, out.Sandbox, out.NetworkAccess, out.Reviewer = approval, sandbox.Type, network, res.Reviewer
		out.Authoritative = approval != "" && sandbox.Type != "" && res.Thread.ID == t.id && res.Thread.Status.Type != ""
		if res.Thread.Status.Type == "active" {
			current.status = proto.StatusWorking
		}
		if len(res.Thread.Status.ActiveFlags) > 0 {
			current.status = proto.StatusBlocked
		}
		return nil
	}
	if err := read(); err != nil {
		return nil, refusal("codex_permissions_unavailable", "Could not read the current Codex policy", nil)
	}
	if !out.Authoritative {
		out.Reason = "readback"
		if choice != nil {
			return nil, refusal("codex_permissions_unsupported", "This Codex version does not report its effective permissions. Update Codex before changing them here.", nil)
		}
		return out, nil
	}
	c.mu.Lock()
	unavailable := c.permissionsUnavailable
	c.mu.Unlock()
	if unavailable {
		out.Reason = "unsupported"
		if choice != nil {
			return nil, refusal("codex_permissions_unsupported", "This Codex connection does not support permission updates", nil)
		}
		return out, nil
	}
	out.Reason = ""
	out.Choices = []string{"read-only", "workspace-write", "full-access"}
	// Working/approval-blocked turns keep their settings; even protocol changes wait for idle.
	if current.status == proto.StatusWorking || current.status == proto.StatusBlocked || current.turn != "" {
		out.Reason = "busy"
		out.Choices = []string{}
	}
	if out.Reason != "busy" {
		c.mu.Lock()
		checked := c.permissionsChecked
		c.mu.Unlock()
		if !checked {
			// All settings omitted: verify the method without changing policy, model or effort.
			if err := c.wire.call(ctx, "thread/settings/update", map[string]any{"threadId": t.id}, nil); err != nil {
				var re *codexError
				if errors.As(err, &re) && re.Code == -32601 {
					c.mu.Lock()
					c.permissionsUnavailable = true
					c.mu.Unlock()
					out.Reason = "unsupported"
					out.Choices = []string{}
					if choice == nil {
						return out, nil
					}
					return nil, refusal("codex_permissions_unsupported", "Codex does not support thread permission updates", nil)
				}
				return nil, refusal("codex_permissions_unavailable", "Could not verify Codex permission controls", nil)
			}
			c.mu.Lock()
			c.permissionsChecked = true
			c.mu.Unlock()
		}
	}
	if choice == nil {
		return out, nil
	}
	if !slices.Contains(out.Choices, *choice) {
		return nil, refusal("codex_permissions_busy", "Wait until Codex is idle and any approval request is resolved before changing permissions.", nil)
	}
	if _, err := codexPermissionFlags(*choice); err != nil || *choice == "" {
		return nil, refusal("codex_permissions_invalid", "Invalid Codex permission choice", nil)
	}
	approval, sandbox := "on-request", "readOnly"
	if *choice == "workspace-write" {
		sandbox = "workspaceWrite"
	}
	if *choice == "full-access" {
		approval, sandbox = "never", "dangerFullAccess"
	}
	if err := c.wire.call(ctx, "thread/settings/update", map[string]any{"threadId": t.id, "approvalPolicy": approval, "approvalsReviewer": "user", "sandboxPolicy": map[string]any{"type": sandbox}}, nil); err != nil {
		var re *codexError
		if errors.As(err, &re) && re.Code == -32601 {
			c.mu.Lock()
			c.permissionsUnavailable = true
			c.mu.Unlock()
		}
		return nil, refusal("codex_permissions_unsupported", "Codex did not accept the permission change. Update Codex if this version does not support thread settings.", nil)
	}
	if err := read(); err != nil {
		return nil, refusal("codex_permissions_unavailable", "Could not read the current Codex policy", nil)
	}
	if !out.Authoritative || out.ApprovalPolicy != approval || out.Sandbox != sandbox || out.Reviewer != "user" || sandbox != "dangerFullAccess" && out.NetworkAccess {
		return nil, refusal("codex_permissions_unconfirmed", "Codex has not confirmed the requested permissions. Refresh to inspect the current policy.", nil)
	}
	n.notifyCodex()
	return out, nil
}

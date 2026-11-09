package node

import (
	"context"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
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

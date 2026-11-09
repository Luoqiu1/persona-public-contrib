package node

import (
	"context"
	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
	"slices"
	"strings"
	"testing"
)

func TestCodexPermissionStartAndLauncherPrecedence(t *testing.T) {
	for _, choice := range []string{"", "read-only", "workspace-write", "full-access"} {
		t.Run(choice, func(t *testing.T) {
			withCodexLaunchers(t, map[string][]string{"cx": {"codex", "-c", "model_reasoning_effort=high"}})
			n, _, _, _ := codexStartHerdr(t, 0)
			_, err := request(t, n, "session.start_codex", map[string]any{"cwd": "~/proj", "launcher": "cx", "prompt": "synthetic start", "codex_permissions": choice, "model": "gpt-6"})
			if err != nil {
				t.Fatal(err)
			}
			flags, _ := codexPermissionFlags(choice)
			for _, flag := range flags {
				if !slices.Contains(ran, flag) {
					t.Fatalf("missing %q in %q", flag, ran)
				}
			}
			if !slices.Contains(ran, "model_reasoning_effort=high") || !slices.Contains(ran, "gpt-6") {
				t.Fatalf("lost model/effort: %q", ran)
			}
			if choice == "" && codexPermissionCarrier(ran) {
				t.Fatalf("default inserted permissions: %q", ran)
			}
		})
	}
	for _, args := range [][]string{
		{"-a", "never"}, {"--sandbox=read-only"}, {"-sonly"}, {"-p", "custom"}, {"--profile=custom"}, {"-pcustom"},
		{"-c", "approval_policy=\"never\""}, {"--config=sandbox_workspace_write.network_access=true"}, {"--full-auto"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			argv := append([]string{"codex"}, args...)
			withCodexLaunchers(t, map[string][]string{"custom": argv})
			n, _, _, _ := codexStartHerdr(t, 0)
			choice := "workspace-write"
			got, err := n.codexLaunchPermissions(context.Background(), nil, "custom", argv, &choice)
			if err == nil {
				t.Fatalf("overrode %q: %q", argv, got)
			}
			choice = ""
			got, err = n.codexLaunchPermissions(context.Background(), nil, "custom", argv, &choice)
			if err != nil || !slices.Equal(got, argv) {
				t.Fatalf("follow changed launcher: %q %v", got, err)
			}
		})
	}
}

func TestCodexPermissionStartRejectsBeforeTerminal(t *testing.T) {
	n, f, _, _ := codexStartHerdr(t, 0)
	withCodexLaunchers(t, map[string][]string{"cx": {"codex", "--profile", "custom"}})
	for _, p := range []map[string]any{
		{"launcher": "cx", "codex_permissions": "full-access"},
		{"launcher": "cx", "codex_permissions": "unknown"},
		{"launcher": "claude", "codex_permissions": ""},
		{"launcher": "cx", "codex_permissions": "", "permission_mode": "bypassPermissions"},
	} {
		p["cwd"], p["prompt"] = "~/proj", "synthetic start"
		if _, err := request(t, n, "session.start_codex", p); err == nil {
			t.Fatalf("accepted %v", p)
		}
	}
	if len(f.trace("tab.create")) != 0 {
		t.Fatal("refused choice opened a terminal")
	}
}

func TestCodexPermissionsFollowOrcaProjectedLaunchProfile(t *testing.T) {
	n, _, o, _, _ := orcaNode(t)
	h := newOrcaHost(&orcart.Client{MetaPath: o.MetaPath})
	settings := map[string]any{"agentDefaultArgs": map[string]string{"codex": "--dangerously-bypass-approvals-and-sandbox -m gpt-6 -c model_reasoning_effort=high"}}
	o.Handle("settings.getAgentLaunch", func(map[string]any) any { return map[string]any{"settings": settings} })
	got, err := n.codexLaunchPermissions(context.Background(), h, "codex", []string{"codex"}, nil)
	if err != nil || !slices.Equal(got, []string{"codex", "--dangerously-bypass-approvals-and-sandbox", "-m", "gpt-6", "-c", "model_reasoning_effort=high"}) {
		t.Fatalf("projected preferences: %q %v", got, err)
	}
	choice := "read-only"
	got, err = n.codexLaunchPermissions(context.Background(), h, "codex", []string{"codex"}, &choice)
	if err != nil || slices.Contains(got, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("explicit choice inherited full access: %q %v", got, err)
	}
	// An alias belongs to its author; Orca defaults must not overwrite its config.
	alias := []string{"codex", "-p", "custom"}
	got, err = n.codexLaunchPermissions(context.Background(), h, "custom", alias, nil)
	if err != nil || !slices.Equal(got, alias) {
		t.Fatalf("alias: %q %v", got, err)
	}
	o.Handle("settings.getAgentLaunch", func(map[string]any) any { return orcarttest.Fail{Code: "method_not_found"} })
	if _, err := n.codexLaunchPermissions(context.Background(), h, "codex", []string{"codex"}, nil); err == nil {
		t.Fatal("old Orca silently fell back")
	}
	for _, call := range o.Trace() {
		if strings.Contains(call, "settings.mutate") || strings.Contains(call, "settings.update") {
			t.Fatalf("global settings changed: %s", call)
		}
	}
}

func TestCodexPermissionOverridePreservesUnrelatedConfig(t *testing.T) {
	n, _, _, _ := codexStartHerdr(t, 0)
	argv := []string{"CODEX_HOME=/configured/permissions", "codex", "-c", `model="permissions-model"`, "--config", `model_provider="sandbox_mode_provider"`}
	choice := "read-only"
	got, err := n.codexLaunchPermissions(context.Background(), nil, "custom", argv, &choice)
	if err != nil || !slices.Equal(got[:len(argv)], argv) {
		t.Fatalf("unrelated config refused or changed: %q %v", got, err)
	}
	for _, args := range [][]string{{"-capproval_policy=never"}, {"--config=\"sandbox_mode\"=\"read-only\""}, {"-c", "sandbox_workspace_write.network_access=true"}} {
		if !codexPermissionCarrier(args) {
			t.Fatalf("permission config not recognized: %q", args)
		}
	}
}

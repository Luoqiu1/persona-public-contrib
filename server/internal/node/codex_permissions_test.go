package node

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
	"github.com/nwverse/persona/server/internal/proto"
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

func TestCodexPermissionsRequireReadbackAndNeverType(t *testing.T) {
	n, f, c := effortPane(t)
	joinPane(t, n)
	// An old server's resume response omits permissions: there is no actionable UI.
	got, err := n.codexPermissions(context.Background(), remoteRaw, nil)
	if err != nil || got.Authoritative || len(got.Choices) != 0 {
		t.Fatalf("old readback: %+v %v", got, err)
	}
	choice := "full-access"
	if _, err = n.codexPermissions(context.Background(), remoteRaw, &choice); err == nil {
		t.Fatal("old server claimed success")
	}
	if f.count("thread/settings/update") != 0 {
		t.Fatal("updated without effective policy readback")
	}
	c.mu.Lock()
	x := c.threads[remoteRaw]
	x.joined = false
	c.threads[remoteRaw] = x
	c.mu.Unlock()
	if _, err = n.codexPermissions(context.Background(), remoteRaw, &choice); err == nil {
		t.Fatal("terminal route claimed success")
	}
	if f.count("turn/start") != 0 || f.count("turn/steer") != 0 {
		t.Fatal("permission change sent a message")
	}
}

func TestCodexPermissionsProtocolReadbackBusyAndConnectionScope(t *testing.T) {
	n, f, c := effortPane(t)
	joinPane(t, n)
	f.mu.Lock()
	f.permissions = map[string]map[string]any{remoteRaw: {"approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": map[string]any{"type": "workspaceWrite"}}}
	f.mu.Unlock()
	c.mu.Lock()
	idle := c.threads[remoteRaw]
	idle.status = proto.StatusDone
	idle.turn = ""
	c.threads[remoteRaw] = idle
	c.mu.Unlock()
	choice := "read-only"
	out, err := n.codexPermissions(context.Background(), remoteRaw, &choice)
	if err != nil || !out.Authoritative || out.Sandbox != "readOnly" || out.ApprovalPolicy != "on-request" {
		t.Fatalf("readback %+v %v", out, err)
	}
	for _, status := range []string{proto.StatusWorking, proto.StatusBlocked} {
		c.mu.Lock()
		x := c.threads[remoteRaw]
		x.status = status
		c.threads[remoteRaw] = x
		c.mu.Unlock()
		before := f.count("thread/settings/update")
		if _, err := n.codexPermissions(context.Background(), remoteRaw, &choice); err == nil {
			t.Fatalf("changed while %s", status)
		}
		if f.count("thread/settings/update") != before {
			t.Fatal("busy updated")
		}
	}
	c.mu.Lock()
	idle = c.threads[remoteRaw]
	idle.status = proto.StatusDone
	idle.turn = ""
	c.threads[remoteRaw] = idle
	c.mu.Unlock()
	f.mu.Lock()
	f.reject = "thread/settings/update"
	f.rejectCode = -32601
	f.mu.Unlock()
	if _, err := n.codexPermissions(context.Background(), remoteRaw, &choice); err == nil {
		t.Fatal("missing method claimed success")
	}
	out, err = n.codexPermissions(context.Background(), remoteRaw, nil)
	if err != nil || len(out.Choices) != 0 || out.Reason != "unsupported" {
		t.Fatalf("capability after refusal %+v %v", out, err)
	}
	c.mu.Lock()
	c.ready = false
	c.mu.Unlock()
	disconnected, err := n.codexPermissions(context.Background(), remoteRaw, nil)
	if err != nil || disconnected.Authoritative || len(disconnected.Choices) != 0 {
		t.Fatalf("disconnected policy was actionable: %+v %v", disconnected, err)
	}
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
	// Reconnects get a new connection object, no pending policy or cross-session fallback.
	if pendingOf(n, remoteRaw) != "" {
		t.Fatal("permission queued as effort")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.got {
		if call.Method == "thread/settings/update" {
			var p map[string]any
			_ = json.Unmarshal(call.Params, &p)
			if p["threadId"] != remoteRaw || p["effort"] != nil || p["model"] != nil {
				t.Fatalf("changed unrelated settings: %s", call.Params)
			}
		}
		if call.Method == "config/value/write" || call.Method == "turn/start" || call.Method == "turn/steer" {
			t.Fatalf("unsafe fallback %s", call.Method)
		}
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

func TestCodexPermissionsProbePreservesOtherThreadsAndTerminalDraft(t *testing.T) {
	n, f, c := effortPane(t)
	terminalNode, terminal, _ := herdrNode(t)
	n.hosts = terminalNode.hosts
	terminal.setScreen("w1:p1", "> a draft that belongs to the terminal user")
	joinPane(t, n)
	f.mu.Lock()
	f.permissions = map[string]map[string]any{remoteRaw: {"approvalPolicy": "never", "approvalsReviewer": "user", "sandbox": map[string]any{"type": "dangerFullAccess"}}, remoteOther: {"approvalPolicy": "on-request", "sandbox": map[string]any{"type": "readOnly"}}}
	f.mu.Unlock()
	out, err := n.codexPermissions(context.Background(), remoteRaw, nil)
	if err != nil || !out.NetworkAccess || !out.Authoritative || len(out.Choices) == 0 {
		t.Fatalf("capability read: %+v %v", out, err)
	}
	updates := f.sent("thread/settings/update")
	if len(updates) != 1 || len(updates[0]) != 1 || updates[0]["threadId"] != remoteRaw {
		t.Fatalf("probe supplied settings: %v", updates)
	}
	choice := "read-only"
	if _, err := n.codexPermissions(context.Background(), remoteRaw, &choice); err != nil {
		t.Fatal(err)
	}
	if calls := terminal.trace("pane.send_input", "pane.send_keys", "agent.prompt"); len(calls) != 0 {
		t.Fatalf("permission control touched a terminal draft: %v", calls)
	}
	f.mu.Lock()
	if f.permissions[remoteOther]["approvalPolicy"] != "on-request" {
		t.Fatal("another thread changed")
	}
	f.mu.Unlock()
	c.mu.Lock()
	if !c.permissionsChecked {
		t.Fatal("capability not recorded on connection")
	}
	c.mu.Unlock()
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

func TestCodexPermissionsRejectIgnoredUpdates(t *testing.T) {
	n, f, _ := effortPane(t)
	joinPane(t, n)
	f.mu.Lock()
	f.permissions = map[string]map[string]any{remoteRaw: {"approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": map[string]any{"type": "workspaceWrite"}}}
	f.ignorePermissions = true
	f.mu.Unlock()
	choice := "full-access"
	if _, err := n.codexPermissions(context.Background(), remoteRaw, &choice); err == nil {
		t.Fatal("a server that ignored policy fields claimed success")
	}
	out, err := n.codexPermissions(context.Background(), remoteRaw, nil)
	if err != nil || out.ApprovalPolicy != "on-request" || out.Sandbox != "workspaceWrite" {
		t.Fatalf("ignored update replaced readback: %+v %v", out, err)
	}
	if pendingOf(n, remoteRaw) != "" {
		t.Fatal("ignored policy was queued for a later turn")
	}
}

func TestCodexPermissionsRejectWrongThreadReadback(t *testing.T) {
	n, f, _ := effortPane(t)
	joinPane(t, n)
	f.mu.Lock()
	f.permissions = map[string]map[string]any{remoteRaw: {"approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": map[string]any{"type": "workspaceWrite"}}}
	f.permissionReadbackThread = remoteOther
	f.mu.Unlock()
	choice := "read-only"
	if _, err := n.codexPermissions(context.Background(), remoteRaw, &choice); err == nil {
		t.Fatal("accepted permissions read back from another thread")
	}
}

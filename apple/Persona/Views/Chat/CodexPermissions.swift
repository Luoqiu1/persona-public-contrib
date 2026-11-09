import SwiftUI

/// Codex approval and filesystem access are separate settings, scoped to one Agent.
nonisolated enum CodexPermissionChoice {
    static let choices = ["read-only", "workspace-write", "full-access"]
    static func title(_ choice: String) -> String {
        switch choice {
        case "read-only", "readOnly": String(localized: "Read Only [Codex Permissions]")
        case "workspace-write", "workspaceWrite": String(localized: "Workspace Access [Codex Permissions]")
        case "full-access", "dangerFullAccess": String(localized: "Full Access [Codex Permissions]")
        default: choice
        }
    }
    static func approval(_ policy: String?) -> String {
        switch policy {
        case "on-request": String(localized: "Asks When Needed [Codex Permissions]")
        case "never": String(localized: "Never Asks [Codex Permissions]")
        default: policy ?? String(localized: "Unknown [Session Modes 4d8c1c]")
        }
    }
    static func detail(_ choice: String) -> String {
        switch choice {
        case "read-only": String(localized: "Reads files; asks before commands that need more access.")
        case "workspace-write": String(localized: "Writes in the workspace; asks before broader access. Network access is restricted.")
        case "full-access": String(localized: "No sandbox restrictions or approval prompts. Applies only to this session.")
        default: String(localized: "Follows the engine or launcher configuration. Custom launcher arguments take precedence.")
        }
    }
}

struct CodexPermissionPicker: View {
    @Binding var choice: String
    var body: some View {
        Section {
            Picker(selection: $choice) {
                Text("Follow Engine or Launcher").tag("")
                ForEach(CodexPermissionChoice.choices, id: \.self) { item in
                    Text(CodexPermissionChoice.title(item)).lineLimit(1).tag(item)
                }
            } label: {
                Label("Codex Permissions", systemImage: Theme.Icon.permissions)
            }
            .accessibilityIdentifier("codexPermissions.creation")
        } header: { Text("Permissions") } footer: {
            Text(CodexPermissionChoice.detail(choice))
        }
    }
}

struct CodexPermissionsButton: View {
    let key: SessionKey
    var action: (() -> Void)? = nil
    @State private var presented = false
    var body: some View {
        Button { if let action { action() } else { presented = true } } label: {
            Label("Codex Permissions", systemImage: Theme.Icon.permissions).lineLimit(1)
        }
        .accessibilityIdentifier("codexPermissions.open")
        .sheet(isPresented: $presented) { CodexPermissionsSheet(key: key) }
    }
}

struct CodexPermissionsSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let key: SessionKey
    @State private var permissions: APIClient.CodexPermissions?
    @State private var error: String?
    @State private var changing = false

    var body: some View {
        NavigationStack {
            Form {
                if let error { Section { Text(error).foregroundStyle(Theme.danger) } }
                if let permissions {
                    CodexPermissionsContent(permissions: permissions) { choice in Task { await load(choice: choice) } }
                } else if error == nil { ProgressView() }
            }
            .themedGroupedPage()
            .navigationTitle("Codex Permissions")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
                ToolbarItem(placement: .primaryAction) { Button { Task { await load() } } label: { ThemedIcon(Theme.Icon.reload) }.accessibilityLabel("Refresh") }
            }
            .disabled(changing)
            .task { await load() }
        }
    }

    private func load(choice: String? = nil) async {
        guard let api = model.api(key.group), !changing else { return }
        changing = true
        defer { changing = false }
        do {
            permissions = try await api.codexPermissions(key, choice: choice)
            error = nil
        } catch {
            self.error = error.shown ?? error.localizedDescription
            // Drop stale actionable choices after a refusal or lost connection.
            permissions = nil
        }
    }
}

/// Effective policy and supported session choices, rendered without owning network state.
struct CodexPermissionsContent: View {
    let permissions: APIClient.CodexPermissions
    var change: (String) -> Void
    var body: some View {
        Section("Current Permissions") {
            LabeledContent("Approval Policy", value: CodexPermissionChoice.approval(permissions.approvalPolicy))
            LabeledContent("Sandbox Access", value: permissions.sandbox.map(CodexPermissionChoice.title) ?? String(localized: "Unknown [Session Modes 4d8c1c]"))
            if permissions.authoritative {
                LabeledContent("Approval Reviewer", value: permissions.reviewer == "user" ? String(localized: "User [Codex Permissions]") : (permissions.reviewer == "guardian_subagent" ? String(localized: "Automatic Review [Codex Permissions]") : (permissions.reviewer ?? String(localized: "Unknown [Session Modes 4d8c1c]"))))
                LabeledContent("Network Access", value: permissions.networkAccess ? String(localized: "Enabled [Codex Permissions]") : String(localized: "Restricted [Codex Permissions]"))
            }
            Text(permissions.authoritative ? String(localized: "Read from the running Codex thread.") : String(localized: "Last recorded policy. This connection cannot verify or change current permissions."))
                .font(Theme.Font.caption).foregroundStyle(.secondary)
        }
        if !permissions.choices.isEmpty {
            Section {
                ForEach(permissions.choices, id: \.self) { choice in
                    Button { change(choice) } label: {
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text(CodexPermissionChoice.title(choice)).lineLimit(1)
                                .foregroundStyle(choice == "full-access" ? Theme.danger : Theme.accent)
                            Text(CodexPermissionChoice.detail(choice)).font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("codexPermissions.\(choice)")
                }
            } header: { Text("This Session Only") }
        } else {
            Section {
                Text(permissions.reason == "busy" ? String(localized: "Wait until Codex is idle and approval requests are resolved.") : String(localized: "Change permissions in the original app or terminal. Update Codex and reconnect for supported controls."))
            }
        }
    }
}

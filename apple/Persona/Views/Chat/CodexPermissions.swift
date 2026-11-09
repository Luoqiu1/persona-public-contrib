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

import SwiftUI

/// Successful manager snapshots survive a transient read failure. No synthetic Agent is ever added to the inventory.
@MainActor @Observable final class WorkspaceCatalog {
    private(set) var workspaces: [HostWorkspace] = []
    private(set) var revision: UInt64 = 0

    func refresh(_ groups: [String], api: (String) -> APIClient?) async {
        var next = workspaces.filter { groups.contains($0.group) }
        for group in groups {
            guard !Task.isCancelled, let client = api(group) else { continue }
            do {
                var rows = try await client.workspaces()
                guard !Task.isCancelled else { return }
                for i in rows.indices { rows[i].group = group }
                next.removeAll { $0.group == group }
                next += rows
            } catch {
                // Old hubs and an unavailable manager retain the existing session-only/last-good view.
            }
        }
        guard !Task.isCancelled else { return }
        if next != workspaces {
            workspaces = next
            revision &+= 1
        }
    }
}

struct WorkspaceCatalogPolling: ViewModifier {
    @Environment(AppModel.self) private var model
    let catalog: WorkspaceCatalog
    let enabled: Bool
    let covered: Bool

    func body(content: Content) -> some View {
        let groups = model.groupIDs
        content.tabActiveTask(id: [String(enabled), String(covered)] + groups) {
            guard enabled, !covered else { return }
            while !Task.isCancelled {
                await catalog.refresh(groups) { model.api($0) }
                do { try await Task.sleep(for: .seconds(10)) } catch { return }
            }
        }
    }
}

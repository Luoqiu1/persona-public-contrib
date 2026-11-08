import SwiftUI

/// Real workspace navigation does not depend on a running Agent. The source identity is also the launch target.
struct HostWorkspaceView: View {
    @Environment(AppModel.self) private var model
    var workspace: HostWorkspace
    @State private var catalog = WorkspaceCatalog()
    @State private var history: [Session] = []
    @State private var cursor: String?
    @State private var hasMore = false
    @State private var loading = false
    @State private var failure: String?
    @State private var showNew = false
    @State private var showChild = false
    @State private var started: SessionKey?
    @State private var openedChild: HostWorkspace?

    private var current: HostWorkspace { catalog.workspaces.first { $0.key == workspace.key } ?? workspace }
    private var online: Bool { model.machine(current.machineRef)?.online == true && current.available }
    private var sessions: [Session] {
        var seen: Set<SessionKey> = []
        return (model.allSessions + history).filter {
            current.contains($0, catalog: catalog.workspaces) && seen.insert($0.key).inserted
        }.sorted { $0.sortDate > $1.sortDate }
    }
    private var children: [HostWorkspace] {
        catalog.workspaces.filter { $0.parentKey == current.key }.sorted {
            ($0.index ?? Int.max, $0.title) < ($1.index ?? Int.max, $1.title)
        }
    }
    var body: some View {
        List {
            Section {
                Text(current.breadcrumb(in: catalog.workspaces)).font(Theme.Font.heading).textSelection(.enabled)
                Text(Fmt.path(current.cwd, home: model.machine(current.machineRef)?.home))
                    .font(Theme.Font.code).foregroundStyle(.secondary).textSelection(.enabled)
                if !online { Label("This computer is offline", systemImage: Theme.Icon.info).foregroundStyle(.secondary) }
            }
            Section {
                Button { showNew = true } label: { Label("New Session [Chats View 58e21b]", systemImage: Theme.Icon.compose) }
                    .buttonStyle(.borderless)
                    .disabled(!online || current.cwd.isEmpty)
                    .accessibilityIdentifier("workspace.newSession")
                Button { showChild = true } label: {
                    Label(String(localized: "workspace.newChild", defaultValue: "New Child Worktree…"), systemImage: Theme.Icon.newWorktree)
                }
                .buttonStyle(.borderless)
                .disabled(!online)
                .accessibilityIdentifier("workspace.newChild")
            }
            if !children.isEmpty {
                Section(String(localized: "workspace.children", defaultValue: "Child Worktrees")) {
                    ForEach(children) { child in
                        NavigationLink(value: child) { WorkspaceChoiceRow(workspace: child, catalog: catalog.workspaces) }
                    }
                }
            }
            Section(String(localized: "workspace.sessions", defaultValue: "Sessions and History")) {
                if sessions.isEmpty && !loading {
                    Text(String(localized: "workspace.noSessions", defaultValue: "No sessions here yet. Start one in this worktree."))
                        .foregroundStyle(.secondary)
                }
                ForEach(sessions) { s in
                    NavigationLink(value: s.key) {
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text(s.title).lineLimit(2)
                            Text(s.state.isLive ? Fmt.relative(s.updatedAt) : String(localized: "workspace.history", defaultValue: "History"))
                                .font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityIdentifier("workspace.session")
                }
                if loading { ProgressView() }
                if let failure {
                    Text(failure).foregroundStyle(Theme.danger)
                    Button("Retry") { Task { await loadHistory(reset: true) } }
                }
                if hasMore {
                    Button(String(localized: "workspace.moreHistory", defaultValue: "Load More History")) {
                        Task { await loadHistory(reset: false) }
                    }.disabled(loading)
                }
            }
        }
        .themedGroupedPage()
        .navigationTitle(current.title)
        .accessibilityIdentifier("workspace.detail")
        .task(id: workspace.key) { await refresh() }
        .refreshable { await refresh() }
        .sheet(isPresented: $showNew) {
            NewChatView(preset: current.preset) { s in showNew = false; started = s.key }
        }
        .sheet(isPresented: $showChild) {
            NewWorktreeSheet(group: current.creationGroup, workspace: current) { made in
                showChild = false
                openedChild = current.createdWorkspace(made)
            }
        }
        .navigationDestination(item: $started) { key in ChatView(key: key).id(key).splitDetailPage(ownsBack: true) }
        .navigationDestination(item: $openedChild) { child in HostWorkspaceView(workspace: child).id(child.key).splitDetailPage() }
    }

    private func refresh() async {
        await catalog.refresh([workspace.group]) { model.api($0) }
        await loadHistory(reset: true)
    }

    private func loadHistory(reset: Bool) async {
        guard !loading, let api = model.api(workspace.group) else { return }
        loading = true
        failure = nil
        defer { loading = false }
        do {
            let query = SessionListQuery(machine: workspace.machineID, text: String(workspace.cwd.prefix(256)))
            let page = try await api.sessionListPage(query, cursor: reset ? nil : cursor)
            try Task.checkCancellation()
            history = reset ? page.sessions : history + page.sessions
            model.landSessionSummaries(page.sessions, group: workspace.group)
            cursor = page.nextCursor
            hasMore = page.hasMore == true && cursor != nil
        } catch {
            if !error.isCancellation { failure = error.shown ?? error.localizedDescription }
        }
    }
}

extension HostWorkspace {
    nonisolated var preset: SpacePreset { SpacePreset(machineID: machineRef.path, cwd: cwd, workspaceID: sourceID, spaceName: title) }

    /// Empty containers and deep descendants use the workspace identity, never an Agent's inferred space.
    nonisolated var creationGroup: SpaceGroup {
        SpaceGroup(id: key, machineID: machineID, spaceID: sourceID, group: group, name: title, index: index ?? 0, sessions: [])
    }

    nonisolated func createdWorkspace(_ made: WorktreeCreated) -> HostWorkspace {
        HostWorkspace(machineID: machineID, source: source, sourceID: made.workspaceID, title: made.branch,
                      cwd: made.path, branch: made.branch, parentSourceID: made.parentWorkspaceID ?? sourceID, group: group)
    }
}

struct WorkspaceChoiceRow: View {
    var workspace: HostWorkspace
    var catalog: [HostWorkspace]

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text(workspace.title).foregroundStyle(Color.primary).lineLimit(1)
            Text(workspace.breadcrumb(in: catalog)).font(Theme.Font.caption).foregroundStyle(Color.secondary).lineLimit(2)
            Text(workspace.cwd).font(Theme.Font.caption).foregroundStyle(Color.secondary).lineLimit(1).truncationMode(.middle)
        }
    }
}

/// Callers scope choices to one machine and source; names and directories are labels, never the selected identity.
struct WorkspacePicker: View {
    var workspaces: [HostWorkspace]
    var selectedID: String?
    var allowDirectory = false
    var onPick: (HostWorkspace?) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    private var shown: [HostWorkspace] {
        workspaces.filter { query.isEmpty || ($0.breadcrumb(in: workspaces) + " " + $0.cwd).localizedStandardContains(query) }
            .sorted { $0.breadcrumb(in: workspaces).localizedStandardCompare($1.breadcrumb(in: workspaces)) == .orderedAscending }
    }

    var body: some View {
        List {
            if allowDirectory {
                Button { onPick(nil); dismiss() } label: {
                    Label(String(localized: "workspace.useDirectory", defaultValue: "Choose by Folder"), systemImage: Theme.Icon.folder)
                }
            }
            ForEach(shown) { workspace in
                Button { onPick(workspace); dismiss() } label: {
                    HStack(spacing: Theme.Space.m) {
                        WorkspaceChoiceRow(workspace: workspace, catalog: workspaces)
                        Spacer(minLength: Theme.Space.xs)
                        if selectedID == workspace.sourceID { Image(systemName: Theme.Icon.success).foregroundStyle(Theme.accent) }
                    }
                }
                .disabled(!workspace.available)
                .accessibilityIdentifier("workspace.choice")
            }
        }
        .themedGroupedPage()
        .navigationTitle(String(localized: "workspace.choose", defaultValue: "Choose Worktree"))
        .searchable(text: $query)
    }
}

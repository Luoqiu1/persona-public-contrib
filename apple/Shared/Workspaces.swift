import Foundation

/// A real host workspace, independent of the agents running in it. Parent IDs are source-local, never paths.
nonisolated struct HostWorkspace: Decodable, Hashable, Identifiable, Sendable {
    var machineID: String
    var source: String
    var sourceID: String
    var title: String
    var cwd: String
    var parentSourceID: String?
    var index: Int?
    var group: String = ""
    var online = true
    var available = true
    var id: String { key }
    var machineRef: MachineRef { MachineRef(group: group, id: machineID) }
    var key: String { machineRef.path + "/" + source + ":" + sourceID }
    var parentKey: String? { parentSourceID.map { machineRef.path + "/" + source + ":" + $0 } }

    enum CodingKeys: String, CodingKey {
        case machineID = "machine_id", source, sourceID = "source_id", title, cwd
        case parentSourceID = "parent_source_id", index, online, available
    }

    /// Explicit Orca placement wins. Older providers may only know a directory; use that only when it is unambiguous.
    func contains(_ session: Session, catalog: [HostWorkspace]) -> Bool {
        guard session.machineRef == machineRef else { return false }
        if let bound = session.workspaceID, !bound.isEmpty { return (session.spaceKind == nil || session.spaceKind == source) && bound == sourceID }
        if session.spaceKind == source { return session.spaceID == sourceID }
        if let kind = session.spaceKind, ["herdr", "herdr-core", "botmux", "paseo"].contains(kind) { return false }
        let path = Self.path(cwd)
        guard !path.isEmpty, Self.path(session.cwd) == path else { return false }
        return catalog.filter { $0.machineRef == machineRef && $0.source == source && Self.path($0.cwd) == path }.count == 1
    }

    func breadcrumb(in catalog: [HostWorkspace]) -> String {
        let byKey = Dictionary(catalog.map { ($0.key, $0) }, uniquingKeysWith: { a, _ in a })
        var names = [title], seen: Set<String> = [key], parent = parentKey
        while let key = parent, seen.insert(key).inserted, let item = byKey[key] {
            names.insert(item.title, at: 0)
            parent = item.parentKey
        }
        return names.joined(separator: " › ")
    }

    private static func path(_ value: String) -> String {
        var value = value
        while value.count > 1 && (value.hasSuffix("/") || value.hasSuffix("\\")) { value.removeLast() }
        return value
    }
}

extension HostWorkspace {
    nonisolated init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        machineID = try c.decode(String.self, forKey: .machineID)
        source = try c.decode(String.self, forKey: .source)
        sourceID = try c.decode(String.self, forKey: .sourceID)
        title = try c.decode(String.self, forKey: .title)
        cwd = try c.decode(String.self, forKey: .cwd)
        parentSourceID = try c.decodeIfPresent(String.self, forKey: .parentSourceID)
        index = try c.decodeIfPresent(Int.self, forKey: .index)
        online = try c.decodeIfPresent(Bool.self, forKey: .online) ?? true
        available = try c.decodeIfPresent(Bool.self, forKey: .available) ?? true
    }
}

extension APIClient {
    nonisolated struct WorkspacesResponse: Decodable, Sendable { var workspaces: [HostWorkspace] }

    func workspaces() async throws -> [HostWorkspace] {
        let response: WorkspacesResponse = try await get("/api/v1/workspaces")
        return response.workspaces
    }
}

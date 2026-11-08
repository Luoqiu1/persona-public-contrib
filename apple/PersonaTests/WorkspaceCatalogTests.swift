import Foundation
import Testing
@testable import Persona

@MainActor struct WorkspaceCatalogTests {
    @Test func refreshScopesParentsToTheHubAndRetainsTheLastSuccessfulSnapshot() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/workspaces", json: #"{"workspaces":[{"machine_id":"m","source":"orca","source_id":"child","parent_source_id":"root","title":"Child","cwd":"/work/child","index":2}]}"#)
        let catalog = WorkspaceCatalog()
        await catalog.refresh(["work"]) { _ in hub.client }
        let item = try #require(catalog.workspaces.first)
        #expect(item.key == "work|m/orca:child")
        #expect(item.parentKey == "work|m/orca:root")
        hub.on("GET", "/api/v1/workspaces") { _ in throw APIError(status: 503, message: "unavailable") }
        await catalog.refresh(["work"]) { _ in hub.client }
        #expect(catalog.workspaces == [item])
        await catalog.refresh([]) { _ in nil }
        #expect(catalog.workspaces.isEmpty, "a removed hub cannot leave its workspace containers behind")
    }

    @Test func oldHubFallsBackWithoutInventingWorkspaceOrAgentRows() async {
        let hub = FakeHub()
        let catalog = WorkspaceCatalog()
        await catalog.refresh([""]) { _ in hub.client }
        #expect(catalog.workspaces.isEmpty)
    }
}

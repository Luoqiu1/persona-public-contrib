import Foundation
import Testing
@testable import Persona

@MainActor struct NewChatDestinationTests {
    private func workspace(source: String) -> HostWorkspace {
        HostWorkspace(machineID: "box", source: source, sourceID: "selected-tree", title: "Selected", cwd: "/work/selected", group: "second-hub")
    }

    private func body(_ request: LocalRequest?) throws -> [String: Any] {
        let data = try #require(request?.body)
        return try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    @Test func workspacePickerKeepsTheSelectedHostForBothStartModes() async throws {
        for (defaultHost, selectedHost) in [("herdr", "orca"), ("orca", "herdr")] {
            let machine = try JSON.decoder.decode(Machine.self, from: Data("""
            {"id":"box","name":"Fixture","kind":"local","online":true,"engine":"\(defaultHost)"}
            """.utf8))
            let picked = workspace(source: selectedHost)
            let destination = try #require(SpacePreset.destination(preset: nil, workspace: picked))
            #expect(destination.host == selectedHost && destination.host != machine.engine.rawValue)
            #expect(destination.machineID == picked.machineRef.path)
            for codex in [true, false] {
                let hub = FakeHub()
                hub.on("POST", "/api/v1/sessions", json: #"{"session":{"machine_id":"box","session_id":"new","title":"Started","cwd":"/work/selected","status":"idle","state":"idle"}}"#)
                _ = try await hub.client.start(.init(machineID: picked.machineID, cwd: destination.cwd,
                                                     launcher: codex ? "codex" : "claude", prompt: codex ? "Check the project" : nil,
                                                     workspaceID: destination.workspaceID, host: destination.host,
                                                     async: codex ? nil : true))
                let sent = try body(hub.sent("POST", "/api/v1/sessions").last)
                #expect(sent["host"] as? String == selectedHost)
                #expect(sent["workspace_id"] as? String == picked.sourceID)
                #expect(sent["cwd"] as? String == picked.cwd)
                #expect(sent["launcher"] as? String == (codex ? "codex" : "claude"))
                #expect(sent["async"] as? Bool == (codex ? nil : true))
            }
        }
    }

    @Test func sidebarPresetWinsOverAnUnrelatedPickerSelection() throws {
        let preset = SpacePreset(machineID: "box", cwd: "/work/sidebar", workspaceID: "sidebar-tree", spaceName: "Sidebar", host: "orca")
        let destination = try #require(SpacePreset.destination(preset: preset, workspace: workspace(source: "herdr")))
        #expect(destination.host == "orca" && destination.workspaceID == "sidebar-tree")
        #expect(destination.cwd == "/work/sidebar" && destination.machineID == "box")
    }

    @Test func directoryAndLegacyStartsKeepTheDefaultHost() throws {
        #expect(SpacePreset.destination(preset: nil, workspace: nil) == nil)
        let legacy = SpacePreset(machineID: "box", cwd: "/work/legacy", workspaceID: "legacy-tree", spaceName: "Legacy")
        #expect(SpacePreset.destination(preset: legacy, workspace: nil)?.host == nil)
        let request = APIClient.StartRequest(machineID: "box", cwd: "/work/directory", launcher: "claude", prompt: nil, workspaceID: nil)
        let json = try #require(JSONSerialization.jsonObject(with: JSON.encoder.encode(request)) as? [String: Any])
        #expect(json["host"] == nil && json["workspace_id"] == nil)
    }
}

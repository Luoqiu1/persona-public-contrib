import Foundation
import Testing
@testable import Persona

@MainActor
struct CodexPermissionsTests {
    @Test func policyAndSandboxDecodeIndependently() throws {
        let data = Data(#"{"approval_policy":"never","sandbox":"workspaceWrite","reviewer":"user","network_access":false,"authoritative":true,"choices":[],"reason":"busy"}"#.utf8)
        let value = try JSONDecoder().decode(APIClient.CodexPermissions.self, from: data)
        #expect(value.approvalPolicy == "never")
        #expect(value.sandbox == "workspaceWrite")
        #expect(!value.networkAccess)
        #expect(value.authoritative)
        #expect(value.choices.isEmpty)
        #expect(value.reason == "busy")
    }

    @Test func creationPreservesWorkspaceHostAndPermission() throws {
        let request = APIClient.StartRequest(machineID: "box", cwd: "/work", launcher: "codex",
                                             workspaceID: "orca:parent", host: "orca", codexPermissions: "read-only", model: "selected-model")
        let value = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: String])
        #expect(value["host"] == "orca")
        #expect(value["workspace_id"] == "orca:parent")
        #expect(value["codex_permissions"] == "read-only")
        #expect(value["model"] == "selected-model")
        #expect(value["permission_mode"] == nil)
    }

    @Test(arguments: [nil, ""] as [String?])
    func defaultCreationUsesLegacyRoute(choice: String?) async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/sessions", json: #"{"session":{"machine_id":"box","session_id":"started"}}"#)
        let session = try await hub.client.start(APIClient.StartRequest(machineID: "box", cwd: "/work", launcher: "codex",
                                                                       workspaceID: "orca:parent", host: "orca", codexPermissions: choice, model: "selected-model"))
        #expect(session.sessionID == "started")
        let requests = hub.sent("POST", "/api/v1/sessions")
        #expect(requests.count == 1)
        #expect(hub.sent("POST", "/api/v1/sessions/codex").isEmpty)
        let data = try #require(requests.first?.body)
        let body = try #require(JSONSerialization.jsonObject(with: data) as? [String: String])
        #expect(body["codex_permissions"] == nil)
        #expect(body["host"] == "orca")
        #expect(body["workspace_id"] == "orca:parent")
        #expect(body["model"] == "selected-model")
    }

    @Test(arguments: ["read-only", "workspace-write", "full-access"])
    func explicitCreationUsesPermissionRoute(choice: String) async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/sessions/codex", json: #"{"session":{"machine_id":"box","session_id":"started"}}"#)
        _ = try await hub.client.start(APIClient.StartRequest(machineID: "box", cwd: "/work", launcher: "codex", codexPermissions: choice))
        #expect(hub.sent("POST", "/api/v1/sessions").isEmpty)
        let requests = hub.sent("POST", "/api/v1/sessions/codex")
        #expect(requests.count == 1)
        let data = try #require(requests.first?.body)
        let body = try #require(JSONSerialization.jsonObject(with: data) as? [String: String])
        #expect(body["codex_permissions"] == choice)
    }

    @Test(arguments: [404, 409])
    func explicitCreationNeverFallsBackAfterRefusal(status: Int) async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/sessions", json: #"{"session":{"machine_id":"box","session_id":"unsafe-fallback"}}"#)
        hub.on("POST", "/api/v1/sessions/codex") { _ in
            throw APIError(status: status, message: "Permission controls require an update")
        }
        do {
            _ = try await hub.client.start(APIClient.StartRequest(machineID: "box", cwd: "/work", launcher: "codex", codexPermissions: "read-only"))
            Issue.record("An explicit permission choice must not fall back to an older creation contract")
        } catch let error as APIError {
            #expect(error.status == status)
        }
        #expect(hub.sent("POST", "/api/v1/sessions/codex").count == 1)
        #expect(hub.sent("POST", "/api/v1/sessions").isEmpty)
    }

    @Test func creationChoicesAreSessionPolicies() {
        #expect(CodexPermissionChoice.choices == ["read-only", "workspace-write", "full-access"])
        #expect(!CodexPermissionChoice.choices.contains("bypassPermissions"))
        #expect(!CodexPermissionChoice.detail("").isEmpty)
        #expect(CodexPermissionChoice.title("read-only") == CodexPermissionChoice.title("readOnly"))
    }

    @Test func historicalPolicyDoesNotInventCurrentAccess() throws {
        let data = Data(#"{"approval_policy":"on-request","sandbox":"workspace-write","network_access":false,"authoritative":false,"choices":[],"reason":"terminal"}"#.utf8)
        let value = try JSONDecoder().decode(APIClient.CodexPermissions.self, from: data)
        #expect(!value.authoritative)
        #expect(value.choices.isEmpty)
    }
}

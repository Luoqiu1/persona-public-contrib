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

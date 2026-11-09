import Foundation
import Testing
@testable import Persona

@MainActor
struct CodexPermissionsTests {
    @Test func creationChoicesAreSessionPolicies() {
        #expect(CodexPermissionChoice.choices == ["read-only", "workspace-write", "full-access"])
        #expect(!CodexPermissionChoice.choices.contains("bypassPermissions"))
        #expect(!CodexPermissionChoice.detail("").isEmpty)
        #expect(CodexPermissionChoice.title("read-only") == CodexPermissionChoice.title("readOnly"))
    }

}

import AppKit
import Observation
import SwiftUI
import Testing
@testable import Persona

@MainActor struct MacWorkspaceSwitchingTests {
    @MainActor @Observable final class Route {
        var path = NavigationPath()
        var workspace: String?
        var displayed = ""
        let agent = SessionKey(machineID: "m", sessionID: "agent")
        func openAgent() { workspace = nil; path = NavigationPath([agent]) }
        func open(_ target: HostWorkspace) { workspace = target.key; path = NavigationPath([target]) }
    }

    struct Destinations: ViewModifier {
        var route: Route
        func body(content: Content) -> some View {
            content
                .navigationDestination(for: SessionKey.self) { key in
                    Text("Agent").onAppear { route.displayed = "agent" }.id(key)
                }
                .navigationDestination(for: HostWorkspace.self) { target in
                    Text(target.title).onAppear { route.displayed = target.sourceID }.id(target.key)
                }
        }
    }

    struct Fixture: View {
        @Bindable var route: Route
        var tree: RepoTree.Worktree
        var body: some View {
            SplitNavigation(path: $route.path, destinations: Destinations(route: route)) {
                List {
                    WorkspaceSidebarControl(open: route.openAgent) {
                        Text("Agent").frame(maxWidth: .infinity, minHeight: 30).contentShape(.rect)
                    } actions: { Button("Agent Actions") {} }
                    .selectionDisabled()
                    WorkspaceSidebarCard(workspace: tree, project: "app", selectedSession: nil,
                                         selectedWorkspace: route.workspace, folded: { _ in false }, toggle: { _ in },
                                         open: route.open, ordered: { $0 }) { _ in EmptyView() } actions: { _, _ in Button("Create Child") {} }
                        .selectionDisabled()
                }
                .padding(.vertical, 60)
            } empty: { Text("Choose Workspace") }
            .environment(\.horizontalSizeClass, .regular)
            .transaction { $0.disablesAnimations = true }
        }
    }

    @Test func titleAndBranchClicksReplaceAnOpenAgentAndEachOther() throws {
        let a = HostWorkspace(machineID: "m", source: "orca", sourceID: "a", title: "Workspace A", cwd: "/code/a", branch: "feature/a")
        let b = HostWorkspace(machineID: "m", source: "orca", sourceID: "b", title: "Workspace B", cwd: "/code/b", branch: "feature/b", parentSourceID: "a")
        let child = RepoTree.Worktree(id: "b", name: b.title, path: b.cwd, main: false, sessions: [], catalog: true, workspace: b)
        let tree = RepoTree.Worktree(id: "a", name: a.title, path: a.cwd, main: false, sessions: [], children: [child], catalog: true, workspace: a)
        let route = Route()
        let host = NSHostingView(rootView: Fixture(route: route, tree: tree))
        host.frame = NSRect(x: 0, y: 0, width: 1200, height: 700)
        let window = NSWindow(contentRect: host.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = host
        window.orderFrontRegardless()
        defer { window.close() }
        host.layoutSubtreeIfNeeded()
        RunLoop.main.run(until: Date().addingTimeInterval(0.1))
        func find(_ view: NSView) -> [MacContextMenuCatcher] {
            (view as? MacContextMenuCatcher).map { [$0] } ?? view.subviews.flatMap(find)
        }
        var dispatching: NSEvent?
        func click(_ index: Int, fraction: CGFloat, shows expected: String) throws {
            RunLoop.main.run(until: Date().addingTimeInterval(0.05))
            host.layoutSubtreeIfNeeded()
            let controls = find(host).sorted { $0.convert(.zero, to: nil).y > $1.convert(.zero, to: nil).y }
            #expect(controls.count == 3)
            let view = try #require(controls.indices.contains(index) ? controls[index] : nil)
            view.scrollToVisible(view.bounds)
            RunLoop.main.run(until: Date().addingTimeInterval(0.05))
            host.layoutSubtreeIfNeeded()
            controls.forEach { $0.currentEvent = { dispatching } }
            let point = view.convert(NSPoint(x: view.bounds.midX, y: view.bounds.height * fraction), to: nil)
            #expect(host.bounds.contains(host.convert(point, from: nil)))
            for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
                dispatching = try #require(NSEvent.mouseEvent(with: type, location: point, modifierFlags: [], timestamp: 0,
                                                              windowNumber: window.windowNumber, context: nil, eventNumber: 0,
                                                              clickCount: 1, pressure: 1))
                window.sendEvent(try #require(dispatching))
            }
            dispatching = nil
            let deadline = Date().addingTimeInterval(1)
            repeat { RunLoop.main.run(until: Date().addingTimeInterval(0.02)) } while route.displayed != expected && Date() < deadline
            #expect(route.displayed == expected, "Both parts of the workspace identity must replace the detail destination")
            var menuTarget: Int?
            for (target, control) in controls.enumerated() {
                control.makeMenu = { NSMenu() }
                control.presentMenu = { _, _, _ in menuTarget = target }
            }
            dispatching = try #require(NSEvent.mouseEvent(with: .rightMouseDown, location: point, modifierFlags: [], timestamp: 0,
                                                         windowNumber: window.windowNumber, context: nil, eventNumber: 0,
                                                         clickCount: 1, pressure: 1))
            window.sendEvent(try #require(dispatching))
            dispatching = nil
            #expect(menuTarget == index, "Title and branch must present the same workspace's menu")
        }
        try click(0, fraction: 0.5, shows: "agent")
        try click(1, fraction: 0.8, shows: "a")
        #expect(route.workspace == a.key)
        try click(2, fraction: 0.2, shows: "b")
        #expect(route.workspace == b.key)
        try click(0, fraction: 0.5, shows: "agent")
        #expect(route.workspace == nil)
        try click(1, fraction: 0.2, shows: "a")
        try click(2, fraction: 0.8, shows: "b")
    }
}

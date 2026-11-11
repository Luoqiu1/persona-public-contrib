// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia;
});
import type { Session } from "../lib/types";
import { setAllFolded } from "../lib/sidebar-api";
import { FoldScope } from "../lib/use-folded";
import { AgentTreeRows } from "./SidebarTree";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement;
let root: Root;
const session = (id: string, patch: Partial<Session> = {}): Session => ({ machine_id: "demo", session_id: id, title: id, cwd: "/code/app", status: "idle", state: "idle", updated_at: "2026-11-11T12:00:00Z", host: "orca", pane_id: `orca:tab:${id}`, ...patch });
const sessions = [session("parent"), session("worker", { parent_agent_id: "parent" }), session("nested", { parent_agent_id: "worker", state: "needs_you", status: "blocked" })];
const show = (activeKey = "") => act(() => root.render(<FoldScope value="agents"><AgentTreeRows sessions={sessions} activeKey={activeKey} scope="demo-group" row={(s) => <a href={`/s/${s.machine_id}/${s.session_id}`}>{s.title}</a>} /></FoldScope>));
beforeEach(() => { localStorage.clear(); host = document.createElement("div"); document.body.append(host); root = createRoot(host); });
afterEach(() => { act(() => root.unmount()); host.remove(); });

it("shows each original session once at its reported depth and leaves its link intact", () => {
  show();
  const parent = host.querySelector('[data-agent-parent="s:demo/parent"]')!;
  const worker = parent.querySelector('[data-agent-parent="s:demo/worker"]')!;
  expect(worker.querySelector('a[href="/s/demo/nested"]')).not.toBeNull();
  expect(host.querySelectorAll("a")).toHaveLength(3);
  expect(parent.querySelector("button")!.getAttribute("aria-expanded")).toBe("true");
});

it("folds repeatedly, counts hidden attention, follows fold-all, and reveals an opened descendant", () => {
  show();
  const parent = host.querySelector('[data-agent-parent="s:demo/parent"]')!;
  const button = parent.querySelector("button")!;
  act(() => button.click());
  expect(button.getAttribute("aria-expanded")).toBe("false");
  expect(parent.textContent).toContain("1");
  expect(parent.querySelector("[inert]")).not.toBeNull();
  act(() => button.click());
  expect(button.getAttribute("aria-expanded")).toBe("true");
  act(() => setAllFolded(true, "agents"));
  expect([...host.querySelectorAll("button[aria-expanded]")].every((b) => b.getAttribute("aria-expanded") === "false")).toBe(true);
  show("s:demo/nested");
  expect([...host.querySelectorAll("button[aria-expanded]")].every((b) => b.getAttribute("aria-expanded") === "true")).toBe(true);
  act(() => setAllFolded(true, "agents"));
  act(() => setAllFolded(false, "agents"));
  expect(button.getAttribute("aria-expanded")).toBe("true");
});

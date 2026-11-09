#!/usr/bin/env python3
"""Run the normal synthetic hub with a stateful Orca worktree migration fixture.

PORT=18869 python3 scripts/mock/mock_worktree_migration.py
All workspaces, provider references and mutations are invented and stay in memory.
GET /mock/worktree-migration reports requests; ?mode=legacy|dirty|unknown selects faults.
"""
import copy
import json
import os
import re
import uuid
from http.server import ThreadingHTTPServer
from urllib.parse import parse_qs, unquote, urlparse

os.environ.setdefault("MOCK_ORCA_CHILDREN", "1")
os.environ.setdefault("MOCK_TERMINAL_PANES", "1")
os.environ.setdefault("MOCK_AGENT_WORKTREE", "1")
os.environ.setdefault("MOCK_START_SECONDS", "0.1")
import mock_hub as hub


ROOT = "app::/Users/me/code/app"
EMPTY = "app::/Users/me/code/app/.worktrees/empty-parent"
LEAF = "app::/Users/me/code/app/.worktrees/catalog-leaf"
MODE = "normal"
REQUESTS = []
RECEIPTS = {}
PREVIEWS = {}
EXTRA = {}
REMOVED = set()
RENAMED = {}
_catalog = hub.mock_workspaces


def catalog():
    rows = copy.deepcopy(_catalog()) + [
        {"id": "fixture-empty", "machine_id": "macbook", "source": "orca", "source_id": EMPTY,
         "parent_source_id": ROOT, "title": "Empty parent", "cwd": EMPTY.split("::", 1)[1],
         "branch": "research/empty", "index": 6, "online": True, "available": True},
        {"id": "fixture-leaf", "machine_id": "macbook", "source": "orca", "source_id": LEAF,
         "parent_source_id": EMPTY, "title": "Catalog leaf", "cwd": LEAF.split("::", 1)[1],
         "branch": "research/leaf", "index": 7, "online": True, "available": True},
    ] + list(copy.deepcopy(EXTRA).values())
    for row in rows:
        if row["source"] == "orca":
            row.update(project_id="example-app", project_name="Example app", project_path="/Users/me/code/app",
                       workspace_kind="folder" if row["source_id"] == "orca:collection" else "worktree",
                       is_main=row["source_id"] == ROOT, head="a" * 40)
        if row["source_id"] in RENAMED:
            row["title"] = RENAMED[row["source_id"]]
    return [row for row in rows if row["source_id"] not in REMOVED]


hub.mock_workspaces = catalog
leaf = copy.deepcopy(next(s for s in hub.SESSIONS if s.get("space_id") == ROOT))
leaf.update(session_id="e0000000-0000-4000-8000-000000000001", title="Catalog-only ancestry check",
            cwd=LEAF.split("::", 1)[1], workspace_cwd=LEAF.split("::", 1)[1], workspace_id=LEAF,
            space_id=LEAF, space_name="Catalog leaf", parent_space_id=EMPTY,
            pane_id="orca:fixture-leaf:p1", slot_id="fixture-leaf-tab", tab_id="fixture-leaf-tab",
            branch="research/leaf", repo_root="/Users/me/code/app", state="idle", status="done")
hub.SESSIONS.append(leaf)


class MigrationHub(hub.H):
    def do_GET(self):
        global MODE
        parsed = urlparse(self.path)
        if parsed.path == "/mock/worktree-migration":
            query = parse_qs(parsed.query)
            if "mode" in query:
                MODE = query["mode"][0]
            return self.send_json({"fixture": "worktree-migration", "mode": MODE, "requests": REQUESTS,
                                   "workspaces": catalog(), "created": len(EXTRA), "removed": sorted(REMOVED)})
        if parsed.path == "/api/v1/workspaces" and MODE == "catalog-offline":
            return self.send_json({"error": "Synthetic manager unavailable", "code": "assistant_offline"}, 503)
        return super().do_GET()

    def do_POST(self):
        path = urlparse(self.path).path
        match = re.fullmatch(r"/api/v1/machines/([^/]+)/worktree/(prepare|create|source|preset|rename|delete)", path)
        if not match:
            return super().do_POST()
        mid, action = unquote(match[1]), match[2]
        body = json.loads(self.body() or b"{}")
        wid = body.get("workspace_id", "")
        REQUESTS.append({"machine": mid, "action": action, "body": body,
                         "op_id": self.headers.get("X-Persona-Op-Id", "")})
        row = next((w for w in catalog() if w["source_id"] == wid and w["machine_id"] == mid), None)
        if not row and not (action == "delete" and body.get("reconcile")):
            return self.send_json({"error": "Synthetic workspace no longer exists", "code": "workspace_missing"}, 409)
        if action == "prepare":
            result = {"root": "/Users/me/code/app", "repoName": "Example app", "branch": row.get("branch", "main"),
                      "path": "/Users/me/code/app/.worktrees", "choose_parent": True, "automatic_name": True}
            if body.get("composer") and MODE != "legacy":
                result["composer"] = {"project": "Example app", "base_ref": "origin/main",
                                      "refs": ["origin/main", "main", "review/existing"],
                                      "presets": [{"id": "web-only", "name": "Web only", "directories": ["web"]}],
                                      "capabilities": ["create", "source.github", "sparse_presets", "branch_override", "folder_parent"]}
            return self.send_json(result)
        if action == "source":
            return self.send_json({"items": [{"type": "pr", "number": 12, "title": "Example review",
                                              "url": "https://github.com/example/app/pull/12",
                                              "branchName": "review/example", "baseRefName": "main"}]})
        if action == "preset":
            return self.send_json({"preset": {"id": "fixture-preset", "name": body["name"],
                                               "directories": body["directories"].splitlines()}})
        if action == "rename":
            RENAMED[wid] = body["name"]
            return self.send_json({"ok": True})
        if action == "create":
            op = self.headers.get("X-Persona-Op-Id", "")
            if op in RECEIPTS:
                return self.send_json(RECEIPTS[op])
            name = body.get("name") or "generated-coral"
            branch = body.get("branch_override") or body.get("branch") or "persona/" + name
            checkout = "/Users/me/code/app/.worktrees/" + re.sub(r"[^a-zA-Z0-9-]", "-", name)
            created_id = "app::" + checkout
            parent = "" if body.get("no_parent") else body.get("parent_workspace_id") or wid
            EXTRA[created_id] = {"id": "fixture-created-" + name, "machine_id": mid, "source": "orca",
                                 "source_id": created_id, "title": name, "cwd": checkout, "branch": branch,
                                 "parent_source_id": parent, "index": 30, "online": True, "available": True}
            result = {"workspace_id": created_id, "parent_workspace_id": parent, "path": checkout, "branch": branch}
            if op:
                RECEIPTS[op] = result
            if MODE == "lost-create":
                return self.send_json({"error": "Synthetic response lost after creation"}, 502)
            return self.send_json(result)
        if body.get("reconcile"):
            removed = wid in REMOVED
            return self.send_json({"workspace_id": wid, "removed": removed, "state": "removed" if removed else "present"})
        if row.get("is_main"):
            return self.send_json({"error": "The main checkout cannot be removed", "code": "main_workspace"}, 409)
        if any(w.get("parent_source_id") == wid for w in catalog()):
            return self.send_json({"error": "This workspace still has children", "code": "worktree_children"}, 409)
        if body.get("preview"):
            token = str(uuid.uuid4())
            PREVIEWS[token] = wid
            return self.send_json({"workspace_id": wid, "kind": "orca", "host_id": "local", "instance_id": "fixture-" + wid,
                                   "path": row["cwd"], "branch": row.get("branch", ""), "dirty": int(MODE == "dirty"),
                                   "folder": row.get("workspace_kind") == "folder", "preview_token": token})
        if PREVIEWS.pop(body.get("preview_token", ""), None) != wid:
            return self.send_json({"error": "Review this workspace again", "code": "preview_changed"}, 409)
        if MODE == "dirty" and not body.get("force"):
            return self.send_json({"error": "This workspace has changed files", "code": "worktree_dirty"}, 409)
        if MODE == "unknown":
            return self.send_json({"workspace_id": wid, "removed": False, "state": "unknown"})
        REMOVED.add(wid)
        return self.send_json({"workspace_id": wid, "removed": True, "state": "removed",
                               "branch_deleted": body.get("delete_branch") is True})


if __name__ == "__main__":
    server = ThreadingHTTPServer(("127.0.0.1", hub.PORT), MigrationHub)
    server.daemon_threads = True
    hub.ENDPOINTS.append(hub.endpoint("127.0.0.1", server.server_port, "http", "lan"))
    print("Synthetic worktree migration hub on 127.0.0.1:%d" % server.server_port, flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()

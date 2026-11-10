import { useEffect, useState } from "react";
import { onOpenNewWorktree, type WorktreeRequest } from "../lib/worktree";
import { lazyOverlay } from "./LazyOverlay";

const NewWorktree = lazyOverlay(() => import("./NewWorktree").then((m) => m.NewWorktree));

/** Keep the creation composer out of the shell's first-screen download. */
export function NewWorktreeHost() {
  const [request, setRequest] = useState<WorktreeRequest | null>(null);
  useEffect(() => onOpenNewWorktree(setRequest), []);
  return request ? <NewWorktree key={JSON.stringify([request.target, request.source, request.parent])} request={request} onClose={() => setRequest(null)} /> : null;
}

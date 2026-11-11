# Sidebar group acceptance

Implementation: `f31689012085045f0c95e285cc2ab3fb76dc1473` (Persona PR #3222, issue #3223). All data and titles in these captures come from the synthetic worktree migration hub; no real account or runtime data is included.

- 40 combinations pass: to-do, computer, repository, project and status, at 1440 / 390 px, light / dark, Standard / maximum Large text.
- Eight additional repository checks confirm nested headings stack beneath their parent without covering Persona.
- Checks cover forward and reverse sibling transitions, scoped collapse / expand controls and client exceptions / page overflow.
- Full Web suite: 8313 passed, 1 skipped. Typecheck, lint, production build and documentation path checks pass. The first-screen budget explicitly reserves 1024 B for shared sticky headings and disclosure controls.
- Reproduce with `scripts/mock/mock_worktree_migration.py`, a dedicated Vite preview, and `node web/e2e/sidebar-groups.mjs <web-origin> <mock-origin> <artifacts>`.
- These are browser checks, not physical iPhone/Safari or screen-reader acceptance. Human screenshot review remains pending before merge.

| Viewport / theme / text | Group controls | Nested sticky heading |
| --- | --- | --- |
| 1440-light-standard | [Controls](1440-light-standard-controls.png) | [Pinned headings](1440-light-standard-repo.png) |
| 1440-light-large | [Controls](1440-light-large-controls.png) | [Pinned headings](1440-light-large-repo.png) |
| 1440-dark-standard | [Controls](1440-dark-standard-controls.png) | [Pinned headings](1440-dark-standard-repo.png) |
| 1440-dark-large | [Controls](1440-dark-large-controls.png) | [Pinned headings](1440-dark-large-repo.png) |
| 390-light-standard | [Controls](390-light-standard-controls.png) | [Pinned headings](390-light-standard-repo.png) |
| 390-light-large | [Controls](390-light-large-controls.png) | [Pinned headings](390-light-large-repo.png) |
| 390-dark-standard | [Controls](390-dark-standard-controls.png) | [Pinned headings](390-dark-standard-repo.png) |
| 390-dark-large | [Controls](390-dark-large-controls.png) | [Pinned headings](390-dark-large-repo.png) |

[All grouping results](results-all.json) · [Nested-header results](results-nested.json)

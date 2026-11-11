# Sidebar controls and workspace title behavior

Implementation `170cc5ac7e23b5932b11884c6bd7da969e65d640`, Persona PR #3222 / issue #3223. Every capture uses the dedicated synthetic fixture.

The Agents title, grouping and quick filters share one sticky toolbar. A single inward-chevron button alternates Collapse all / Expand all, with a tooltip and spoken label naming its next action. Parent/child section headings stack below the toolbar; the Persona gradient no longer fades the first heading. Workspace titles select their exact target and toggle the same disclosure as their arrows at every depth, including workspaces with a direct Agent. Each Agent opens from its own row.

- 205 focused sidebar/fold tests pass after integration. Typecheck, lint, production build and documentation checks pass.
- 40 browser combinations pass across five groupings, desktop/phone, light/dark and Standard/Large.
- 22 full worktree-flow checks pass, including repeated child-title clicks and Enter/Space toggles, exact Agent navigation and the existing creation/rename/removal flows.
- Upstream merge validation before this UI repair passed 8708 Web tests (one skipped), focused Go worktree tests/vet and both synthetic browser suites. Full i18n still has an independently reproduced upstream parked.go baseline failure; it is not a fully green server suite.
- No hosted CI or PR merge is claimed. Human screenshot review, physical iPhone/Safari and screen-reader acceptance remain pending.

| Layout | Controls | Sticky groups |
| --- | --- | --- |
| 1440-light-standard | [Controls](1440-light-standard-controls.png) | [Groups](1440-light-standard-repo.png) |
| 1440-light-large | [Controls](1440-light-large-controls.png) | [Groups](1440-light-large-repo.png) |
| 1440-dark-standard | [Controls](1440-dark-standard-controls.png) | [Groups](1440-dark-standard-repo.png) |
| 1440-dark-large | [Controls](1440-dark-large-controls.png) | [Groups](1440-dark-large-repo.png) |
| 390-light-standard | [Controls](390-light-standard-controls.png) | [Groups](390-light-standard-repo.png) |
| 390-light-large | [Controls](390-light-large-controls.png) | [Groups](390-light-large-repo.png) |
| 390-dark-standard | [Controls](390-dark-standard-controls.png) | [Groups](390-dark-standard-repo.png) |
| 390-dark-large | [Controls](390-dark-large-controls.png) | [Groups](390-dark-large-repo.png) |

[Grouping results](group-results.json) · [Full workspace results](worktree-results.json)

# Web worktree acceptance evidence

Source revision: `02909fb6691af9028165dec8fe9a8a307160a1ad`. All data, computer names, paths and messages in these captures come from the synthetic mock fixture. No live account or hub is shown.

The runner passed 22 checks per text setting (44 total) at 1440px and 390px in light and dark. Large is the largest setting currently exposed by the web app (14/13 text scale); this does not establish physical iPhone or system accessibility-size acceptance. Phone creation deliberately delays the first composer chunk by 100ms.

[Verification summary](verification.json) · [Standard results](standard/results.json) · [Large results](large/results.json)

The full Web suite retains one upstream readiness assertion failure, independently reproduced on unmodified main. The publication revision passes the 62 focused worktree/shell/startup/catalog checks, production build, startup budgets, lint and the recorded focused Go checks.

## Screenshots

| Screen | Desktop light | Desktop dark | Phone light | Phone dark |
| --- | --- | --- | --- | --- |
| standard: catalog | [1440-light](standard/1440-light-catalog.png) | [1440-dark](standard/1440-dark-catalog.png) | [390-light](standard/390-light-catalog.png) | [390-dark](standard/390-dark-catalog.png) |
| standard: exact-destination | [1440-light](standard/1440-light-exact-destination.png) | [1440-dark](standard/1440-dark-exact-destination.png) | [390-light](standard/390-light-exact-destination.png) | [390-dark](standard/390-dark-exact-destination.png) |
| standard: composer | [1440-light](standard/1440-light-composer.png) | [1440-dark](standard/1440-dark-composer.png) | [390-light](standard/390-light-composer.png) | [390-dark](standard/390-dark-composer.png) |
| standard: created | [1440-light](standard/1440-light-created.png) | [1440-dark](standard/1440-dark-created.png) | [390-light](standard/390-light-created.png) | [390-dark](standard/390-dark-created.png) |
| standard: rename | [1440-light](standard/1440-light-rename.png) | [1440-dark](standard/1440-dark-rename.png) | [390-light](standard/390-light-rename.png) | [390-dark](standard/390-dark-rename.png) |
| standard: delete-dirty | [1440-light](standard/1440-light-delete-dirty.png) | [1440-dark](standard/1440-dark-delete-dirty.png) | [390-light](standard/390-light-delete-dirty.png) | [390-dark](standard/390-dark-delete-dirty.png) |

Additional standard recovery captures: [uncertain creation](standard/1440-light-create-uncertain.png), [uncertain removal](standard/1440-light-delete-uncertain.png).

| large: catalog | [1440-light](large/1440-light-catalog.png) | [1440-dark](large/1440-dark-catalog.png) | [390-light](large/390-light-catalog.png) | [390-dark](large/390-dark-catalog.png) |
| large: exact-destination | [1440-light](large/1440-light-exact-destination.png) | [1440-dark](large/1440-dark-exact-destination.png) | [390-light](large/390-light-exact-destination.png) | [390-dark](large/390-dark-exact-destination.png) |
| large: composer | [1440-light](large/1440-light-composer.png) | [1440-dark](large/1440-dark-composer.png) | [390-light](large/390-light-composer.png) | [390-dark](large/390-dark-composer.png) |
| large: created | [1440-light](large/1440-light-created.png) | [1440-dark](large/1440-dark-created.png) | [390-light](large/390-light-created.png) | [390-dark](large/390-dark-created.png) |
| large: rename | [1440-light](large/1440-light-rename.png) | [1440-dark](large/1440-dark-rename.png) | [390-light](large/390-light-rename.png) | [390-dark](large/390-dark-rename.png) |
| large: delete-dirty | [1440-light](large/1440-light-delete-dirty.png) | [1440-dark](large/1440-dark-delete-dirty.png) | [390-light](large/390-light-delete-dirty.png) | [390-dark](large/390-dark-delete-dirty.png) |

Additional large recovery captures: [uncertain creation](large/1440-light-create-uncertain.png), [uncertain removal](large/1440-light-delete-uncertain.png).


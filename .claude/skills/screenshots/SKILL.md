---
name: screenshots
description: Refresh the simple-cd UI screenshots (light and dark) used in the README and the docs with web/scripts/screenshots.mjs. Use after a visible UI change or when asked for new or additional screenshots.
---

# Screenshots

`web/scripts/screenshots.mjs` drives the installed Google Chrome with `playwright-core` and writes `docs/assets/screenshots/<name>.png` and `<name>-dark.png`.

## Prerequisites

Two installations must run, both reached on 127.0.0.1:

```sh
make dev-up dev-seed      # dev stack with demo data; alice holds the lock on demo-test for one hour
make web run              # server on http://127.0.0.1:8080, rebuilt with the current UI
make e2e-up e2e           # end-to-end stack on http://127.0.0.1:18080 with a real deployment log and audit log
```

`make run` serves the UI embedded at build time: rebuild (`make web run`) after UI changes. Rerun `make dev-seed` if the lock has expired, otherwise the target screenshot shows no lock.

## Take them

```sh
make screenshots
```

Then look at every changed PNG before committing, in both themes. Shots from the dev stack: projects, project, target, deploy-dialog, user-menu. From the e2e stack: deployment, admin.

## Add a screenshot

1. Add a `shot(...)` call in `screenshots.mjs`, with a `prepare` function for dialogs or menus. Wait for real conditions (`waitForSelector`, `networkidle`), not fixed timeouts. Native dialogs match `dialog[open]`.
2. Reference it in Markdown with a `<picture>` element so GitHub picks the theme:

   ```html
   <picture>
     <source media="(prefers-color-scheme: dark)" srcset="docs/assets/screenshots/NAME-dark.png">
     <img alt="what the screenshot shows" src="docs/assets/screenshots/NAME.png">
   </picture>
   ```

   In Jekyll pages use `{{ '/assets/screenshots/NAME.png' | relative_url }}`.
3. Keep the PNGs small (the set is about 2.5 MB); the script uses a 1280 x 800 viewport at scale 2.

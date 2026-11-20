# draw.io DSL reference

All coordinates are draw.io pixels; x grows right, y grows down. Boxes are `(x, y, w, h)`.

## Setup

```python
from pathlib import Path
import drawio_dsl as dsl
from drawio_dsl import *          # Diagram, building blocks, colors, edge styles
dsl.OUT_DIR = Path(__file__).resolve().parent
```

Run with plain `python3 diagrams.py` (not `python3 -I`: isolated mode drops the script folder from `sys.path` and the import fails). `--png <dir>` writes 2x PNG previews for inspection (the folder is created; `services/cost` becomes `services-cost.png`); keep previews out of the repository.

## Inspecting

For each preview the script prints `pixel = 2 * (coordinate - (min_x, min_y) + 16)`: the PNG is scaled 2x, offset by the top left of the drawing, with a 16 px border that is scaled too.
To check a tight spot at diagram box `(x, y, w, h)`, crop pixels `left = 2 * (x - min_x + 16)`, `top = 2 * (y - min_y + 16)`, width `2 * w`, height `2 * h`:

- macOS: `sips -c <height> <width> --cropOffset <top> <left> in.png --out crop.png`
- elsewhere: `python3 -c "from PIL import Image; Image.open('in.png').crop((left, top, left + width, top + height)).save('crop.png')"`

## API

| Call | Draws | Returns |
|------|-------|---------|
| `Diagram(name)` | new diagram; `name` may contain a sub folder (`services/cost`) | diagram `d` |
| `d.save()` | writes `<name>.drawio` and a light-mode `<name>.svg` with the diagram embedded (opens in draw.io) | |
| `d.vertex(x, y, w, h, html, style)` | any shape | id |
| `d.edge(src, tgt, style, points=[], exit=(rx, ry), entry=(rx, ry))` | connector between shapes; `exit`/`entry` are relative positions on the box, `points` are absolute waypoints | id |
| `d.line(x1, y1, x2, y2, style, points=[])` | connector not attached to shapes (association class links, gutters) | id |
| `d.text(x, y, w, h, html, align, size, bold)` | free label | id |
| `uml_class(d, x, y, w, name, attributes, methods, stereotype)` | class, enum («enumeration»), interface («interface»); height is computed; empty compartments are skipped | `(id, box)` |
| `hassoc(d, a, a_box, b, b_box, y, left, name, right, style=ASSOC)` | straight horizontal association at height `y` with multiplicities and italic name above the line | |
| `component(d, x, y, w, h, name, detail, stereotype="component", external=False)` | component; `detail` = grey responsibility line; `external=True` = grey fill. External systems: `stereotype="subsystem", external=True`, in component and deployment diagrams alike | id |
| `subsystem(d, x, y, w, h, name, size=18)` | own subsystem, purple, title top left; `"\n"` in `name` breaks the title | id |
| `ball(d, cx, cy, label, label_side, label_w)` | provided interface centred on `(cx, cy)`; `label=""` and a `d.text()` when no side is free | id |
| `node(d, x, y, w, h, stereotype, name, tech, fill)` | deployment node (3D box), `tech` in purple; `fill=EXTERNAL_FILL` for external systems | id |
| `actor(d, x, y, name)` | UML actor for users outside the model, name below | id |
| `group(d, x, y, w, h, title_html)` | flat dashed group, e.g. Kubernetes namespace | |
| `artifact(d, x, y, w, h, name, detail)` | «artifact» with document icon | id |
| `note(d, x, y, w, h, text)` | yellow note | id |
| `legend(d, x, y, w, [(kind, text)])` | legend; kinds: `subsystem`, `component`, `external`, `namespace`, `interface`, `technology`, `responsibility`, `note` | |
| `service_spec(file, name, provider, operations, data)` | separate diagram: provider component, «interface» with operations, data types | |
| `frac(box, y)` | relative y on a box side for absolute `y`, for `exit`/`entry` | float |

Edge styles: `LINE` (provider to ball, communication path), `SOCKET` (required interface; the half circle wraps the ball it ends on), `ASSOC`, `COMPOSITION` (diamond at source), `DEPENDENCY` (dashed, open arrow), `DASHED`.

Labels are HTML (`<b>`, `<i>`, `<br/>`); names passed to building blocks are escaped. To show `<version>` inside `node()`'s `tech`, write `&lt;version&gt;`.

## Layout recipes

- **Grid first.** Pick column x positions and row y positions before placing anything (e.g. columns `xs = [400, 740, 1080]`, width 240, gap 100). Align client components above the server components that serve them, so service lines run straight down.
- **Services between rows.** Put balls in the gap between two rows (e.g. y = 270 between a container ending at 210 and one starting at 330). Provider line from the provider's top, socket line from the user's bottom: both straight.
- **Internal services between columns.** Ball in the middle of the horizontal gap, provider on one side, user on the other, label centred above the ball (two lines, about 100 px wide).
- **Shared socket.** A second user joins the first user's line with a T: route it to a point on that line (`points=[...]`) so both end on the same socket. If two users join from opposite sides, give them different heights so no "+" appears.
- **Socket from the side.** When the user is not above or below the ball, route an L: `d.edge(user, ball, SOCKET, exit=(0.5, 1), entry=(0, 0.5), points=[(x_user, y_ball)])`. Fine when the grid does not fit (e.g. 2 client components over 3 server components); prefer it over an empty grid slot.
- **Deployment paths.** Attach paths to the node border, never to the subsystem inside it, or the line crosses the node's stereotype and technology text. External systems are grey nodes (`fill=EXTERNAL_FILL`) with the grey subsystem inside.
- **Users outside the model.** Place an `actor()` next to the public service and connect it with a `SOCKET` edge.
- **Straight lines.** Compute `exit`/`entry` with `frac(box, y)` from one absolute `y` for both ends; `hassoc()` does this for associations.
- **Container titles.** A title sits top left, about 60 px high (stereotype plus name). Keep the first row 70 to 90 px below the container top. Title width is about 9 px per character at 18 px bold and 7.5 px at 15 px, starting 16 px from the left border. If a service line would pass within 15 px of the title, use a two-line title (`"simple-cd\nServer"`, then start the first row about 110 px below the top), or exit and enter off-centre (`exit=((x_line - x) / w, 1)`) so the line runs further right.
- **Labels in gaps.** A path label needs a gap: about 80 px for two short lines, 160 px for "Report, Contract, Financing Service / HTTPS :443". Plan the gap instead of shrinking the label.
- **Infrastructure links.** Services used by a whole subsystem (authentication, persistence) may attach to the subsystem border; list the actual users in the README table.
- **Detours.** When a line cannot avoid crossing, route it around the outside of a container (e.g. along the top inside the client container, 20 px above the components) rather than through other components.
- **Compactness.** After the first render, shrink containers to their content, move legends and external systems into empty corners, and put small enums side by side.

## Pitfalls

- draw.io exports adaptive `light-dark()` colors; `d.save()` pins light mode. Do not remove that post-processing.
- `edgeStyle=orthogonalEdgeStyle` reroutes when `exit`/`entry` are missing; always set both for predictable lines.
- A ball with sockets on two adjacent sides shows overlapping arcs; merge the users into one line instead.
- The 3D `node()` cube needs 80 px above its first child (or a `group()` border) for stereotype, name and one technology line; add 20 px per extra technology line. At 60 px the technology line is hidden.
- `--embed-diagram` makes the SVG editable in draw.io, but lasting changes go into `diagrams.py`; regenerated files overwrite manual edits.

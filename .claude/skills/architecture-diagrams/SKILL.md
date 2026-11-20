---
name: architecture-diagrams
description: Change the simple-cd architecture views - UML object model, component, deployment and service specs drawn with the hand-placed draw.io DSL in docs/architecture/uml, and the PlantUML sequence and state diagrams in docs/architecture/diagrams. Use when a change adds or moves components, services, deployment nodes or domain concepts, or when asked for readable diagrams.
---

# Architecture diagrams

Two kinds of views:

| Views | Source | Render |
|---|---|---|
| Object model, components (server, executor), deployment, service specs | `docs/architecture/uml/diagrams.py` with `drawio_dsl.py` | `python3 docs/architecture/uml/diagrams.py` (needs the `drawio` desktop CLI) |
| Sequence, activity and state diagrams | `docs/architecture/diagrams/*.puml` | `docs/architecture/diagrams/render.sh` (PlantUML in Docker) |

Commit the sources together with the generated `.drawio` and `.svg` files. Never edit generated files by hand.
The DSL API and layout recipes are in [DSL.md](DSL.md) next to this file. The existing functions in `diagrams.py` are the examples to follow.

## Steps

1. **Model first.** Write down the components, the services each provides, and who uses each service. Check the modeling rules below.
2. **Place by hand.** Every shape gets fixed coordinates in one function per view. Never use auto-layout (PlantUML or Mermaid for structure, draw.io `--layout`): it produces crossing lines and labels on lines.
3. **Render and look.** `python3 docs/architecture/uml/diagrams.py --png <scratch dir>` writes PNG previews. Look at every changed view, crop tight spots, and walk the checklist. Fix and re-render until every item passes.
4. **Document.** Update the tables in `docs/architecture/index.md` (components, deployment nodes, paths) and `docs/architecture/services.md` or `data-model.md`. A decision about structure, technology or protocol gets an ADR in `docs/adr/`.

## Views

| View | Shows | Never shows |
|---|---|---|
| Object model | domain concepts, attributes, domain operations, associations with multiplicities, enums | ids, version columns, audit tables, technical types |
| Component diagram | subsystems, components, services as ball (provided) and socket (required) | technologies, protocols, ports |
| Deployment diagram | every subsystem of the component diagram, own and external, on its node; technologies, versions, ports; each path labeled with service and protocol | internal wiring of a subsystem |
| Service spec | one service as UML interface with operations and data types | anything else |

## Modeling rules

- One job per component; dependencies inside a subsystem point one way, no cycles; nobody reads another component's data.
- Components inside a subsystem are connected; an isolated component is a modeling error.
- Data has one owner: the owner provides a recording service and writers call it.
- One lollipop per service; every user connects to that one ball.
- Meaningful names ("Delivery Service", "Job Control Service"), never "Server" or "Web Client".
- Shallow deployment nesting: node, sub-node, subsystem. In Kubernetes, put image, ports and runtime on the pod; a namespace is a dashed group, not a node.
- Infrastructure the platform handles (for example the Gateway terminating TLS) is not modeled by hand.

## Style rules

- Light mode only; colors carry meaning and every colored diagram has a legend: purple = own subsystem, grey fill = external system, purple text = technology (deployment only), grey text = responsibility (components only), dashed purple = namespace or group, yellow = note.
- Compact, but readability wins: space a line or its label needs is not wasted.
- UML notation: stereotypes, component icon, ball-and-socket, multiplicities, composition diamonds, dashed dependencies, dog-eared notes.

## Checklist

- [ ] Every text is readable at 100 % and lies on white or fill, not on a line or other text.
- [ ] Container titles are not crossed by lines or touched by children.
- [ ] In deployment nodes the technology line is fully visible (80 px from node top to first child).
- [ ] No two lines cross; shared sockets join with staggered T junctions, never a "+".
- [ ] At most one socket arc per ball side.
- [ ] Associations are straight where the boxes allow it, labels above the line.
- [ ] Each service lollipop appears once; all users are connected; no component is isolated.
- [ ] The deployment diagram contains every subsystem of the component diagram and labels each path with service and protocol; paths end on node borders.
- [ ] Colors match the legend; the legend lists every color used.
- [ ] No large empty regions; names are meaningful.

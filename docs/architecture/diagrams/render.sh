#!/bin/sh
# Renders every PlantUML diagram in this folder to an SVG next to its source.
# Needs Docker. Run it after changing a .puml file and commit both files.
set -eu
PLANTUML_IMAGE="plantuml/plantuml:1.2026.9"
cd "$(dirname "$0")"
docker run --rm -v "$PWD":/data -w /data "$PLANTUML_IMAGE" -tsvg -charset UTF-8 *.puml

---
title: Design record (German)
nav_order: 9
---

# CD-Server - Design

Stand: 2026-11-20 (nach Grilling)

## Ziel

Ein schlanker Delivery-Server: Er baut und testet nichts, er deployt nur.
Er sieht fertige Artefakte aus GitHub, und berechtigte Nutzer spielen sie per Klick auf ein Target aus.

## Entscheidungen im Überblick

| Thema | Entscheidung |
|---|---|
| Stack | Server in Go, UI in React |
| Betrieb | Läuft in Kubernetes, zustandslos, mehrere Replicas |
| Datenbank | Postgres |
| Projekt | Genau ein GitHub-Repo, darf mehrere Images haben |
| Target | Gehört zu genau einem Projekt, `id` ist der stabile Schlüssel |
| Forks | Werden abgelehnt |
| Registry | Nur erlaubte Präfixe pro Projekt, globaler Default |
| Repo-Konfiguration | `.cd.yaml` im Repo (statisch), `deploy.json` pro Build (dynamisch) |
| Herkunft | `repo`, `sha`, Quelle und PR leitet der Server aus GitHub ab |
| Auth | Keycloak OIDC, Client-Roles eines `cd-server`-Clients |
| Rollen | Pro Projekt `developer` und `maintainer`, optionale Target-Rolle, `platform-admin` |
| Keycloak-Rollen anlegen | v1 von Hand, mit Doku und Skript |
| Sichtbarkeit | Alle Eingeloggten sehen alles, Deployen braucht Rollen |
| Target-Verwaltung | YAML in Git, als ConfigMap ausgerollt, Reload zur Laufzeit |
| Target-Typen | `compose` (eine Datei auf VM), `helm` (Kubernetes), `ansible` (niedrige Prio) |
| Helm-Chart | Pflicht: aus einer OCI-Registry |
| Helm-Images | Abbildung per `set` mit Platzhaltern in `.cd.yaml` |
| Previews | Helm-Target mit `preview: true`, Namespace pro PR |
| Auslösung | Manuell; Auto-Deploy über Regeln am Target (v1 für Previews) |
| Parallelität | Ein laufender Deploy pro Slot, bei Auto-Deploy ein Warteplatz (neuester gewinnt) |
| Locking | Opt-in pro Target, Default 1 h, beginnt mit Deploy-Start |
| Wartung | Blockiert alle Deploys, auch von Maintainern |
| Secrets | Vault; der Deploy-Job holt sie selbst über Vault-Kubernetes-Auth |
| Audit-Log | Ja, Aufbewahrung konfigurierbar, sichtbar für `platform-admin` |
| Benachrichtigungen | Zurückgestellt |

## Architektur

```
GitHub Repo
  ├─ .cd.yaml (statisch: Chart, Image-Abbildung, Values-Dateien, Compose-Pfad)
  └─ CI (Sache der Entwickler) baut Images / Chart
       └─ Action "cd-publish": schreibt deploy.json, validiert, lädt Artefakt "cd-deploy" hoch
                    │  Webhook (workflow_run, pull_request, release) über GitHub App
                    ▼
CD-Server (Go, N Replicas)  ── Postgres
  ├─ GitHub App: holt deploy.json und .cd.yaml zum SHA, setzt Deployment-Status im PR
  ├─ Vollständigkeitsprüfung
  ├─ Target-Config (ConfigMap, Reload zur Laufzeit)
  ├─ React-UI: Projekt → Artefakt → Target → Deploy, Target-Status, Logs per SSE
  ├─ Keycloak OIDC
  └─ legt pro Deploy einen Kubernetes Job an
         Executor (Go-Binary im Job): holt Secrets aus Vault, dann
         ├─ compose → SSH auf VM, Compose-Datei und .cd.env kopieren, pull, up -d
         ├─ helm    → `helm upgrade --install oci://... --version ... -f values.yaml`
         └─ ansible → `ansible-playbook` mit Extra-Vars (eigenes Image)
```

## Vertrauensgrenze

- Der Server verarbeitet nur Repos, auf denen die GitHub App installiert ist **und** die in der Target-Config als Projekt stehen.
- Webhook-Signaturen werden mit dem Webhook-Secret der App geprüft.
- Webhooks mit `workflow_run.head_repository.full_name` ungleich dem Repo selbst (Fork-PRs) werden verworfen und im Audit-Log vermerkt.
- Für private Repos wird in der Org "Allow forking" ausgeschaltet.
- Images und Charts müssen unter einem erlaubten Registry-Präfix des Projekts liegen.
- Herkunftsdaten (`repo`, `sha`, Quelle, PR-Nummer) stammen aus GitHub, nie aus `deploy.json`.

## Repo-Konfiguration

### `.cd.yaml` (statisch, im Repo, vom Server zum SHA gelesen)

```yaml
helm:
  chart: oci://ghcr.io/org/charts/app
  set:                       # für alle Helm-Targets
    server.image.repository: "{images.server.repository}"
    server.image.digest:     "{images.server.digest}"
    client.image:            "{images.client.ref}"
  targets:                   # nur für ein Target, überschreibt helm.set
    app-previews:
      values: deploy/values-preview.yaml
      set:
        ingress.host: "pr-{cd.pr}.app.preview.example.org"
    app-test:
      values: deploy/values-test.yaml
compose:
  path: deploy/docker-compose.yml
ansible:
  playbook: deploy/site.yml
```

Platzhalter sind reine Textersetzung ohne Template-Logik:
`{images.<name>.ref|repository|digest|tag}` und `{cd.sha|pr|target|namespace}`.
Ist ein Platzhalter im Kontext leer (z.B. `{cd.pr}` außerhalb einer Preview), ist das Artefakt für dieses Target unvollständig.

### `deploy.json` (pro Build, von der Action als Artefakt `cd-deploy` hochgeladen)

```json
{
  "version": 1,
  "images": [
    { "name": "server", "ref": "ghcr.io/org/app-server@sha256:..." },
    { "name": "client", "ref": "ghcr.io/org/app-client@sha256:..." }
  ],
  "helm": { "version": "1.4.0-pr42.abc123" },
  "autoDeploy": ["app-previews"]
}
```

Laden mehrere Runs für denselben SHA ein `cd-deploy` hoch (Re-Run oder mehrere Workflows), gewinnt der neueste.
Bei mehreren Workflows zeigt die UI eine Warnung.

### Herkunft

| Quelle | Erkennung |
|---|---|
| `pr` | Run aus einem PR, Nummer aus `workflow_run.pull_requests`, sonst `GET /commits/{sha}/pulls` |
| `branch` | Push-Run auf einen Branch |
| `release` | Run durch `release`-Event oder Tag-Push |

## Vollständigkeit

Ein Artefakt ist für ein Target deploybar, wenn:

1. der Workflow-Run erfolgreich war,
2. alle Images per Digest in der Registry existieren und unter einem erlaubten Präfix liegen,
3. der Helm-Chart in der Version existiert und unter einem erlaubten Präfix liegt (falls Helm),
4. alle Platzhalter im Kontext des Targets einen Wert haben (falls Helm),
5. die Compose-Datei zum SHA existiert, jedes Image als `${<NAME>_IMAGE}` nutzt und kein `build:` enthält (falls Compose),
6. das Playbook existiert (falls Ansible mit Quelle `repo`).

Geprüft wird beim Eingang und unmittelbar vor dem Deploy noch einmal (nur die Registry-Existenz).
Unvollständige Artefakte erscheinen mit Grund in der UI, sind aber nicht deploybar.

## Helm-Values

Reihenfolge, der spätere Eintrag gewinnt:

1. Defaults des Charts
2. Values-Datei aus dem Repo für dieses Target (`.cd.yaml` → `helm.targets.<id>.values`)
3. Values aus der Target-Config bzw. Vault
4. Generierte Werte aus `helm.set`, ergänzt um `helm.targets.<id>.set`

Der Executor schreibt alles in eine Datei und übergibt sie mit `-f`.
Vor dem Deploy zeigt die UI den Diff der zusammengeführten Values (ohne Vault-Secrets) gegenüber dem letzten Deploy.
Ändert sich mehr als die Images, verlangt sie eine Bestätigung.

## Compose

- Ein Compose-Target überträgt genau eine Datei. Wer mehr braucht, nimmt Ansible.
- Config-Dateien und Secrets liegen bereits auf der VM (`.env` und Weiteres).
- Der Executor schreibt die Image-Digests in eine eigene `.cd.env` und ruft per SSH auf der VM `docker compose --env-file .env --env-file .cd.env pull` und danach `up -d` auf.
- Durch Digests statt Tags läuft garantiert genau die neue Variante.
- Vor dem Deploy zeigt die UI den Diff der Compose-Datei gegenüber dem letzten erfolgreichen Deploy und verlangt bei Änderung eine Bestätigung.

## Target-Config

YAML im Config-Repo, als ConfigMap mit dem Helm-Chart des CD-Servers ausgerollt.
Sie beschreibt, was ein Target ist, nicht in welchem Zustand es gerade ist.

```yaml
defaults:
  lockDuration: 1h
  jobTimeout: 30m
  previewTTL: 72h
  logRetention: 90d
  auditRetention: 365d
  artifactRetention: 30d       # geschlossene PRs
  keepBranchArtifacts: 50      # pro Branch bzw. für Releases
  registries: [ghcr.io/org/]

projects:
  org/app:
    registries: [ghcr.io/org/] # optional, sonst Default

targets:
  - id: app-test
    project: org/app
    type: helm
    lockable: true             # Default false
    cluster: vault:secret/cd/clusters/test
    namespace: app
    release: app
    values: vault:secret/cd/values/app-test

  - id: app-previews
    project: org/app
    type: helm
    preview: true              # Namespace pro PR, nicht lockbar
    autoDeploy:
      from: [pr]
    cluster: vault:secret/cd/clusters/dev
    previewTTL: 48h

  - id: app-staging            # später
    project: org/app
    type: helm
    autoDeploy:
      from: [branch]
      branches: [main]
    cluster: vault:secret/cd/clusters/staging
    namespace: app

  - id: app-prod
    project: org/app
    type: compose
    role: deploy:app-prod      # optionale zusätzliche Rolle
    host: vm1.example.org
    ssh: vault:secret/cd/ssh/app-prod
    dir: /opt/app
    jobTimeout: 10m

  - id: tools-vm
    project: org/tools
    type: ansible
    ansible:
      source: git@github.com:org/infra.git   # oder "repo" = App-Repo zum SHA
      ref: main
      playbook: playbooks/tools.yml
      inventory: inventories/prod
      credentials: vault:secret/cd/ssh/tools
```

### Laden und Ändern

- Der Server beobachtet die gemountete Datei und lädt sie ohne Neustart neu.
- Eine ungültige Config wird nie aktiv: Der Server behält die alte, loggt den Fehler und zeigt `platform-admin` ein Banner.
- `cd-server validate config.yaml` läuft als CI-Check im Config-Repo.
- Ein gelöschtes Target wird nicht aufgeräumt: Laufende Deploys laufen zu Ende, Historie bleibt, Preview-Namespaces erscheinen als "verwaist" und werden von `platform-admin` per Klick gelöscht.
- Ein umbenanntes Target gilt als gelöscht und neu angelegt.

## Target-Zustand

- **Statisch (YAML):** Existenz, Typ, Projekt, `lockable`, Dauer, Timeouts.
- **Laufzeit (Postgres):** Wartung (Grund, wer, seit) und Lock (Halter, bis).

Die UI zeigt pro Target einen Status, der erste zutreffende gewinnt:

| Status | Bedeutung | Wer darf deployen |
|---|---|---|
| `maintenance` | Von Hand gesperrt, mit Grund | Niemand, auch kein Auto-Deploy |
| `deploying` | Ein Deploy läuft (bei Previews: pro PR) | Niemand im selben Slot |
| `locked` | Gesperrt, mit Halter und Restzeit | Halter; Maintainer und `platform-admin` per Override |
| `available` | Frei | Developer, Maintainer |

Wartung schalten Maintainer und `platform-admin` in der UI an und aus.
Wer während der Wartung deployen will, beendet sie zuerst.

## Deploy-Slots und Parallelität

- Ein Slot ist bei normalen Targets das Target selbst, bei Preview-Targets das Paar aus Target und PR.
- Pro Slot läuft höchstens ein Deploy: `UNIQUE (target_id, slot) WHERE status IN ('pending', 'running')`.
- Auto-Deploy hat einen Warteplatz pro Slot. Ein neuer Auftrag ersetzt einen wartenden, der neueste gewinnt.
- Manuelle Deploys auf einen belegten Slot werden abgelehnt.

## Auto-Deploy

- Das Repo fordert per `deploy.json` → `autoDeploy` an, das Target erlaubt per Regel (`from`, optional `branches`).
- Passt die Regel nicht, wird übersprungen, mit Grund im GitHub-Deployment-Status und im Audit-Log.
- Auf Targets in `maintenance` oder `locked` wird übersprungen.
- Akteur im Audit-Log: `auto-deploy` plus `workflow_run.triggering_actor`.

## Locks

1. Der Lock beginnt mit dem Start des Deploys und läuft `lockDuration`. Prüfen und Setzen passieren in einer Transaktion.
2. Der Lock bleibt auch bei `failed` oder `timed_out` bestehen.
3. Der Halter darf erneut deployen (verlängert den Lock) und kann früher freigeben.
4. Auto-Deploy erzeugt nie einen Lock.
5. Ein Override macht den Maintainer zum neuen Halter, die Dauer beginnt neu, der Verlauf ist sichtbar.

## Rollen (Keycloak Client-Roles)

| Rolle | Darf |
|---|---|
| `project:<repo>:developer` | Deployen auf `available`-Targets des Projekts, Previews starten, eigenen Lock freigeben |
| `project:<repo>:maintainer` | Alles von Developer, Locks überschreiben, Wartung schalten |
| Target-Rolle, z.B. `deploy:app-prod` | Zusätzlich nötig, wenn das Target `role` setzt |
| `platform-admin` | Alles in allen Projekten, Audit-Log, verwaiste Previews aufräumen |

Ohne Rolle sehen eingeloggte Nutzer alles nur lesend.
Rollen für neue Repos werden v1 von Hand angelegt, mit einem Skript (`kcadm.sh`) und einer Anleitung.

## Executor und Secrets

- Pro Deploy ein Kubernetes Job mit eigenem ServiceAccount.
- Ein Go-Binary im Job meldet sich per Vault-Kubernetes-Auth an, liest nur die Pfade seines Targets und legt sie in ein `emptyDir` mit `medium: Memory`.
- Secrets landen nie in etcd, und der Server selbst hat keinen Lesezugriff darauf.
- v1: eine Vault-Rolle für alle Executor-Jobs, beschränkt auf `secret/cd/*`. Eine Rolle pro Target erst bei Bedarf.
- Zwei Images: `executor` (helm, kubectl, ssh) und `executor-ansible` (ansible-core, git, ssh). Eine Docker-CLI braucht der Executor nicht: `docker compose` läuft per SSH auf der VM.
- Überschreitet ein Job `jobTimeout`, bricht der Server ihn ab (`timed_out`).

## Rechte in Ziel-Clustern

- **Normale Helm-Targets:** ServiceAccount mit `admin`, gebunden nur an den Ziel-Namespace. Den Namespace legt der Betrieb an.
- **Preview-Targets:** ServiceAccount darf Namespaces anlegen und löschen und hat `admin` in diesen.
  Der Server löscht nur Namespaces mit `cd/managed=true` und passenden Labels `cd/target` und `cd/pr`, sonst Abbruch mit Audit-Eintrag.
- Preview-Cluster enthalten keine produktiven Workloads.
- Namespace-Name: `pr-<repo>-<pr>`, Repo-Name bei Bedarf gekürzt und mit kurzem Hash ergänzt, maximal 63 Zeichen.

## Previews

- Manuell per Klick oder per Auto-Deploy.
- Der Executor installiert den Chart in den Namespace des PRs und der Server meldet ihn als GitHub Deployment am PR.
- Ein neuer Push aktualisiert die Preview und setzt die TTL zurück.
- Bei PR-Close oder nach Ablauf der TTL löscht der Server Release und Namespace.
- DNS, Ingress und Quotas regelt das Ziel-Cluster.

## Artefakt-Lebenszyklus

- `deploy.json` wird beim Webhook sofort in Postgres gesichert, der Ablauf bei GitHub spielt dann keine Rolle.
- PR-Artefakte verschwinden nach PR-Close aus der Standardansicht und werden nach `artifactRetention` gelöscht.
- Branch- und Release-Artefakte: die letzten `keepBranchArtifacts` pro Branch bzw. für Releases bleiben.
- Ein Artefakt, das auf einem Target läuft oder dort zuletzt erfolgreich war, wird nie gelöscht.
- Löscht die Registry ein Image, zeigt die UI "Image fehlt".

## Betrieb

- Hintergrundarbeit (TTL, Timeouts, Aufbewahrung, Warteplätze, Job-Beobachtung) läuft auf genau einer Replica, gewählt per Postgres Advisory Lock. *(Selbst entschieden)*
- Job-Logs werden live per SSE an die UI gestreamt, nach Abschluss in Postgres gespeichert und nach `logRetention` gelöscht. *(Selbst entschieden)*
- Audit-Events werden nach `auditRetention` gelöscht.

## Datenmodell (v1)

- `artifacts`: Repo, SHA, Quelle, PR, Run-ID, deploy.json, `.cd.yaml`-Snapshot, Vollständigkeitsstatus
- `deployments`: Artefakt, Target, Slot, Auslöser, Status, Log, Snapshot (Compose-Datei bzw. Values ohne Secrets)
- `deploy_queue`: Target, Slot, Artefakt (ein Eintrag pro Slot)
- `target_state`: Target, Wartung (Grund, wer, seit), Lock (Halter, bis)
- `lock_history`: Target, Halter, von, bis, Override durch
- `previews`: Repo, PR, Target, Namespace, läuft ab um, verwaist
- `audit_events`: Zeitpunkt, Akteur, Aktion, Objekt, Details

## Onboarding eines Repos

1. GitHub App auf dem Repo installieren.
2. Projekt und Targets in die Target-Config eintragen.
3. Keycloak-Rollen per Skript anlegen.
4. `.cd.yaml` ins Repo legen und die Action `cd-publish` in den Workflow einbauen.
5. Für Compose-Targets: `.env` und Config-Dateien auf der VM anlegen, Compose-Datei auf `${<NAME>_IMAGE}` umstellen.

## Nicht-Ziele

Builds, Tests, Pipelines, Approval-Workflows, Multi-Stage-Promotion, eigene Build-Skripte, mehrere Dateien bei Compose, DNS/Ingress/Quotas für Previews, Admin-UI für Targets, automatisches Anlegen von Keycloak-Rollen, Fork-PRs.

## Voraussetzungen außerhalb des Projekts

- Vault-Kubernetes-Auth (harte Voraussetzung für den ersten Deploy)
- Getrennte Preview-Cluster mit Wildcard-DNS, Ingress und Quotas
- ServiceAccounts und Kubeconfigs in den Ziel-Clustern, in Vault abgelegt
- "Allow forking" in der Org für private Repos ausgeschaltet

## Offene Punkte

1. Benachrichtigungen (zurückgestellt).
2. Automatisches Anlegen von Keycloak-Rollen (zurückgestellt).

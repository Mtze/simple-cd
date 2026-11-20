"""UML views of simple-cd, hand-placed with the draw.io DSL.

    python3 docs/architecture/uml/diagrams.py               # writes .drawio and .svg next to this file
    python3 docs/architecture/uml/diagrams.py --png <dir>   # also writes PNG previews for checking

Needs the draw.io desktop CLI (`drawio`) on the PATH. Lasting changes go here, not into
the generated files.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import drawio_dsl as dsl  # noqa: E402
from drawio_dsl import *  # noqa: E402,F403

dsl.OUT_DIR = Path(__file__).resolve().parent


def vassoc(d, a, a_box, b, b_box, x, top="", name="", bottom="", style=ASSOC):
    """Straight vertical association at x from a (above) to b (below).
    Multiplicities and the italic name sit right of the line."""
    d.edge(a, b, style, exit=(round((x - a_box[0]) / a_box[2], 4), 1), entry=(round((x - b_box[0]) / b_box[2], 4), 0))
    y1, y2 = a_box[1] + a_box[3], b_box[1]
    if top:
        d.text(x + 6, y1 + 2, 60, 20, top)
    if bottom:
        d.text(x + 6, y2 - 22, 60, 20, bottom)
    if name:
        d.text(x + 8, (y1 + y2) / 2 - 10, 150, 20, f"<i>{name}</i>")


# ---------------------------------------------------------------- object model

def object_model():
    d = Diagram("object-model")
    # column 1: who and what is delivered
    user, ub = uml_class(d, 40, 40, 280, "User", ["name: String", "roles: String [*]"],
                         ["mayDeploy(target: Target): Boolean", "mayTakeOver(target: Target): Boolean",
                          "mayMaintain(target: Target): Boolean"])
    project, pb = uml_class(d, 40, 250, 280, "Project", ["name: String", "allowedRegistries: String [1..*]"])
    artifact, ab = uml_class(d, 40, 420, 280, "Artifact",
                             ["commit: String", "source: Source", "ref: String", "pullRequest: Integer [0..1]",
                              "chartVersion: String [0..1]"],
                             ["readiness(target: Target): String [*]"])
    image, ib = uml_class(d, 40, 660, 280, "Image", ["name: String", "reference: String"])

    # column 2: the target and its runtime state
    lock, lb = uml_class(d, 440, 40, 250, "Lock", ["until: DateTime"],
                         ["extend()", "release()", "takeOver(by: User)"])
    maint, mb = uml_class(d, 740, 40, 250, "Maintenance", ["reason: String", "since: DateTime"], ["end()"])
    target, tb = uml_class(d, 440, 250, 550, "Target",
                           ["name: String", "kind: TargetKind", "lockable: Boolean", "lockDuration: Duration",
                            "jobTimeout: Duration", "requiredRole: String [0..1]"],
                           ["status(): TargetStatus"])
    deploy, db = uml_class(d, 440, 520, 260, "Deployment",
                           ["kind: DeploymentKind", "status: DeploymentStatus", "startedBy: String",
                            "started: DateTime [0..1]", "finished: DateTime [0..1]", "log: Text"])
    preview, prb = uml_class(d, 790, 520, 250, "Preview",
                             ["pullRequest: Integer", "namespace: String", "expires: DateTime"],
                             ["resetTTL()", "tearDown()"])
    rule, rb = uml_class(d, 1110, 250, 300, "AutoDeployRule", ["sources: Source [1..*]", "branches: String [*]"],
                         ["allows(artifact: Artifact): Boolean"])

    # enumerations, side by side
    ex, ey = 1110, 450
    uml_class(d, ex, ey, 140, "Source", ["pullRequest", "branch", "release"], stereotype="enumeration")
    uml_class(d, ex + 160, ey, 140, "TargetKind", ["helm", "compose", "ansible"], stereotype="enumeration")
    uml_class(d, ex, ey + 150, 140, "TargetStatus", ["maintenance", "deploying", "locked", "available"],
              stereotype="enumeration")
    uml_class(d, ex + 160, ey + 150, 140, "DeploymentKind", ["deploy", "teardown"], stereotype="enumeration")
    uml_class(d, 790, 700, 250, "DeploymentStatus", ["pending", "running", "succeeded", "failed", "timedOut"],
              stereotype="enumeration")

    hassoc(d, user, ub, lock, lb, 80, "1", "holds", "0..*")
    hassoc(d, project, pb, target, tb, 300, "1", "has", "*", style=COMPOSITION)
    hassoc(d, target, tb, rule, rb, 300, "1", "", "0..1", style=COMPOSITION)
    hassoc(d, artifact, ab, deploy, db, 560, "0..1", "deploys", "*")
    hassoc(d, deploy, db, preview, prb, 560, "*", "into", "0..1")
    vassoc(d, user, ub, project, pb, 180, "*", "member of", "*")
    vassoc(d, project, pb, artifact, ab, 180, "1", "publishes", "*", style=COMPOSITION)
    vassoc(d, artifact, ab, image, ib, 180, "1", "contains", "*", style=COMPOSITION)
    vassoc_up(d, target, tb, lock, lb, 565, "1", "0..1")
    vassoc_up(d, target, tb, maint, mb, 865, "1", "0..1")
    vassoc(d, target, tb, deploy, db, 570, "1", "receives", "*")
    vassoc(d, target, tb, preview, prb, 915, "1", "hosts", "*")
    note(d, 1110, 370, 300, 46, "Previews exist only on targets of kind helm marked as preview targets.")
    d.save()


# ---------------------------------------------------------------- components

def components_server():
    """Logical view of the server. Persistence and the registry serve the whole subsystem
    and attach to its border; the README lists the actual users."""
    d = Diagram("components-server")
    server = subsystem(d, 300, 260, 880, 470, "simple-cd\nServer")
    session = component(d, 340, 380, 220, 70, "Session Manager", "logins, sessions, roles")
    api = component(d, 640, 380, 220, 70, "Delivery API", "views, deploy requests, live logs")
    intake = component(d, 940, 380, 220, 70, "Artifact Intake", "webhooks, provenance, snapshots")
    launcher = component(d, 340, 570, 220, 70, "Job Launcher", "executor jobs, job logs")
    rules = component(d, 640, 570, 220, 70, "Delivery Rules", "readiness, locks, slots, auto-deploy")
    sched = component(d, 940, 570, 220, 70, "Deployment Scheduler", "starts and watches deployments")
    api_box, intake_box, sched_box = (640, 380, 220, 70), (940, 380, 220, 70), (940, 570, 220, 70)

    # Session Service: between Session Manager and Delivery API
    svc = ball(d, 600, 415, "")
    d.text(560, 336, 80, 40, "Session<br/>Service", align="center", size=13, bold=True)
    d.edge(session, svc, LINE, exit=(1, 0.5), entry=(0, 0.5))
    d.edge(api, svc, SOCKET, exit=(0, 0.5), entry=(1, 0.5))

    # Delivery Rules Service: API straight down, Intake and Scheduler join with T junctions
    rsvc = ball(d, 750, 510, "")
    d.text(766, 492, 140, 40, "Delivery Rules<br/>Service", size=13, bold=True)
    d.edge(rules, rsvc, LINE, exit=(0.5, 0), entry=(0.5, 1))
    d.edge(api, rsvc, SOCKET, exit=(0.5, 1), entry=(0.5, 0))
    d.edge(intake, rsvc, SOCKET, exit=(round(20 / 220, 4), 1), entry=(0.5, 0), points=[(960, 462), (750, 462)])
    d.edge(sched, rsvc, SOCKET, exit=(round(40 / 220, 4), 0), entry=(0.5, 0), points=[(980, 476), (750, 476)])

    # Job Control Service: Launcher provides; API routes down the column gap, Scheduler joins below Rules
    jsvc = ball(d, 600, 605, "")
    d.text(436, 650, 156, 40, "Job Control<br/>Service", align="right", size=13, bold=True)
    d.edge(launcher, jsvc, LINE, exit=(1, 0.5), entry=(0, 0.5))
    d.edge(api, jsvc, SOCKET, exit=(0, frac(api_box, 440)), entry=(0.5, 1),
           points=[(620, 440), (620, 690), (600, 690)])
    d.edge(sched, jsvc, SOCKET, exit=(round(110 / 220, 4), 1), entry=(0.5, 1),
           points=[(1050, 670), (620, 670), (620, 690), (600, 690)])

    # outside the server
    console = subsystem(d, 640, 40, 220, 110, "simple-cd\nConsole", size=15)
    keycloak = component(d, 340, 40, 220, 100, "Keycloak Identity Provider", stereotype="subsystem", external=True)
    github = component(d, 880, 40, 460, 110, "GitHub", "repositories, workflow runs, deployments",
                       stereotype="subsystem", external=True)
    kube = component(d, 20, 560, 190, 90, "Kubernetes Control Plane", stereotype="subsystem", external=True)
    registry = component(d, 1320, 650, 200, 80, "OCI Registry", stereotype="subsystem", external=True)
    database = subsystem(d, 640, 840, 220, 80, "simple-cd\nDatabase", size=14)

    dsvc = ball(d, 750, 210, "")
    d.text(770, 190, 90, 40, "Delivery<br/>Service", size=13, bold=True)
    d.edge(api, dsvc, LINE, exit=(0.5, 0), entry=(0.5, 1))
    d.edge(console, dsvc, SOCKET, exit=(0.5, 1), entry=(0.5, 0))

    auth = ball(d, 450, 210, "")
    d.text(290, 190, 140, 40, "Authentication<br/>Service", align="right", size=13, bold=True)
    d.edge(keycloak, auth, LINE, exit=(0.5, 1), entry=(0.5, 0))
    d.edge(session, auth, SOCKET, exit=(0.5, 0), entry=(0.5, 1))
    d.edge(console, auth, SOCKET, exit=(0, round(80 / 110, 4)), entry=(0.5, 1),
           points=[(600, 120), (600, 245), (450, 245)])

    hook = ball(d, 1000, 210, "")
    d.text(890, 190, 90, 40, "Webhook<br/>Service", align="right", size=13, bold=True)
    d.edge(intake, hook, LINE, exit=(round(60 / 220, 4), 0), entry=(0.5, 1))
    d.edge(github, hook, SOCKET, exit=(round(120 / 460, 4), 1), entry=(0.5, 0))

    content = ball(d, 1100, 210, "")
    d.text(1120, 190, 140, 40, "Repository<br/>Content Service", size=13, bold=True)
    d.edge(github, content, LINE, exit=(round(220 / 460, 4), 1), entry=(0.5, 0))
    d.edge(intake, content, SOCKET, exit=(round(160 / 220, 4), 0), entry=(0.5, 1))

    status = ball(d, 1300, 605, "")
    d.text(1320, 585, 160, 40, "Deployment<br/>Status Service", size=13, bold=True)
    d.edge(github, status, LINE, exit=(round(420 / 460, 4), 1), entry=(0.5, 0))
    d.edge(sched, status, SOCKET, exit=(1, 0.5), entry=(0, 0.5))

    reg = ball(d, 1250, 690, "")
    d.text(1190, 638, 120, 40, "Registry<br/>Service", align="center", size=13, bold=True)
    d.edge(registry, reg, LINE, exit=(0, 0.5), entry=(1, 0.5))
    d.edge(server, reg, SOCKET, exit=(1, frac((300, 260, 880, 470), 690)), entry=(0, 0.5))

    persist = ball(d, 750, 790, "")
    d.text(770, 770, 120, 40, "Persistence<br/>Service", size=13, bold=True)
    d.edge(database, persist, LINE, exit=(0.5, 0), entry=(0.5, 1))
    d.edge(server, persist, SOCKET, exit=(round(450 / 880, 4), 1), entry=(0.5, 0))

    work = ball(d, 255, 605, "")
    d.text(212, 548, 86, 40, "Workload<br/>Service", align="center", size=13, bold=True)
    d.edge(kube, work, LINE, exit=(1, 0.5), entry=(0, 0.5))
    d.edge(launcher, work, SOCKET, exit=(0, 0.5), entry=(1, 0.5))

    note(d, 20, 690, 250, 60, "Runs the simple-cd Executor as one Job per deployment (executor view).")
    legend(d, 20, 40, 250, [("subsystem", "Own subsystem"), ("component", "Component"),
                            ("external", "External system"), ("interface", "Required / provided service"),
                            ("responsibility", "Responsibility"), ("note", "Note")])
    d.save()


def components_executor():
    """Logical view of the executor and the publishing action."""
    d = Diagram("components-executor")
    subsystem(d, 300, 260, 880, 400, "simple-cd\nExecutor")
    loader = component(d, 640, 380, 220, 70, "Secret Loader", "Vault login, secret files")
    helm = component(d, 340, 560, 220, 70, "Helm Deployer", "releases, preview namespaces")
    compose = component(d, 640, 560, 220, 70, "Compose Deployer", "compose file, image variables")
    ansible = component(d, 940, 560, 220, 70, "Ansible Deployer", "playbook checkout and run")

    secrets = ball(d, 750, 500, "")
    d.text(770, 476, 130, 40, "Secret Access<br/>Service", size=13, bold=True)
    d.edge(loader, secrets, LINE, exit=(0.5, 1), entry=(0.5, 0))
    d.edge(compose, secrets, SOCKET, exit=(0.5, 0), entry=(0.5, 1))
    d.edge(helm, secrets, SOCKET, exit=(round(180 / 220, 4), 0), entry=(0.5, 1), points=[(520, 535), (750, 535)])
    d.edge(ansible, secrets, SOCKET, exit=(round(20 / 220, 4), 0), entry=(0.5, 1), points=[(960, 545), (750, 545)])

    vault = component(d, 640, 40, 220, 100, "HashiCorp Vault", stereotype="subsystem", external=True)
    vsvc = ball(d, 750, 210, "")
    d.text(770, 190, 120, 40, "Secret<br/>Service", size=13, bold=True)
    d.edge(vault, vsvc, LINE, exit=(0.5, 1), entry=(0.5, 0))
    d.edge(loader, vsvc, SOCKET, exit=(0.5, 0), entry=(0.5, 1))

    for x, name, svc, user in [(340, "Target Cluster", "Release<br/>Service", helm),
                               (640, "Compose Host", "Compose Host<br/>Service", compose),
                               (940, "Managed Hosts", "Managed Host<br/>Service", ansible)]:
        ext = component(d, x, 790, 220, 80, name, stereotype="subsystem", external=True)
        b = ball(d, x + 110, 725, "")
        d.text(x + 130, 705, 130, 40, svc, size=13, bold=True)
        d.edge(ext, b, LINE, exit=(0.5, 0), entry=(0.5, 1))
        d.edge(user, b, SOCKET, exit=(0.5, 1), entry=(0.5, 0))

    registry = component(d, 20, 550, 190, 90, "OCI Registry", stereotype="subsystem", external=True)
    reg = ball(d, 255, 595, "")
    d.text(212, 538, 86, 40, "Registry<br/>Service", align="center", size=13, bold=True)
    d.edge(registry, reg, LINE, exit=(1, 0.5), entry=(0, 0.5))
    d.edge(helm, reg, SOCKET, exit=(0, 0.5), entry=(1, 0.5))

    github = component(d, 1320, 550, 240, 90, "GitHub", stereotype="subsystem", external=True)
    content = ball(d, 1250, 595, "")
    d.text(1186, 530, 128, 40, "Repository<br/>Content Service", align="center", size=13, bold=True)
    d.edge(github, content, LINE, exit=(0, 0.5), entry=(1, 0.5))
    d.edge(ansible, content, SOCKET, exit=(1, 0.5), entry=(0, 0.5))

    publish = subsystem(d, 1330, 280, 220, 90, "cd-publish Action", size=15)
    upload = ball(d, 1440, 460, "")
    d.text(1460, 440, 120, 40, "Artifact<br/>Upload Service", size=13, bold=True)
    d.edge(github, upload, LINE, exit=(round(120 / 240, 4), 0), entry=(0.5, 1))
    d.edge(publish, upload, SOCKET, exit=(0.5, 1), entry=(0.5, 0))

    note(d, 20, 290, 250, 76, "The Kubernetes Control Plane starts the executor as a Job for the Job Launcher (server view).")
    legend(d, 20, 40, 250, [("subsystem", "Own subsystem"), ("component", "Component"),
                            ("external", "External system"), ("interface", "Required / provided service"),
                            ("responsibility", "Responsibility"), ("note", "Note")])
    d.save()


# ---------------------------------------------------------------- deployment

def deployment():
    d = Diagram("deployment")
    cluster = node(d, 440, 300, 780, 600, "executionEnvironment", "Delivery Cluster", "Kubernetes 1.37")
    group(d, 460, 390, 740, 270, "<b>namespace simple-cd</b> · Helm chart simple-cd")
    app = node(d, 480, 430, 330, 210, "pod", "simple-cd",
               "Go 1.27 · distroless · :8080<br/>replicas: 2 · Service · HTTPRoute :443")
    subsystem(d, 500, 540, 290, 75, "simple-cd Server", size=15)
    job = node(d, 850, 430, 330, 210, "pod", "simple-cd-executor Job",
               "Go 1.27 · helm 4.3 · kubectl 1.37 · ssh<br/>one Job per deployment")
    subsystem(d, 870, 540, 290, 75, "simple-cd Executor", size=15)
    apiserver = node(d, 480, 720, 240, 160, "executionEnvironment", "API Server", "Kubernetes 1.37 · :6443",
                     fill=EXTERNAL_FILL)
    component(d, 495, 800, 210, 65, "Kubernetes Control Plane", stereotype="subsystem", external=True)

    browser = node(d, 40, 330, 300, 160, "executionEnvironment", "Web Browser", "React 19 · TypeScript")
    subsystem(d, 60, 410, 260, 60, "simple-cd Console", size=14)
    keycloak = node(d, 40, 580, 300, 150, "device", "Keycloak", "Keycloak 26 · OIDC", fill=EXTERNAL_FILL)
    component(d, 55, 660, 270, 55, "Keycloak Identity Provider", stereotype="subsystem", external=True)
    postgres = node(d, 40, 800, 300, 150, "device", "PostgreSQL", "PostgreSQL 18 · :5432")
    subsystem(d, 60, 880, 260, 55, "simple-cd Database", size=14)

    runner = node(d, 240, 40, 300, 170, "executionEnvironment", "GitHub Actions Runner", "GitHub Actions",
                  fill=EXTERNAL_FILL)
    subsystem(d, 260, 120, 260, 70, "cd-publish Action", size=14)
    github = node(d, 700, 40, 360, 170, "device", "github.com", "REST API · Webhooks · Git", fill=EXTERNAL_FILL)
    component(d, 715, 120, 330, 70, "GitHub", stereotype="subsystem", external=True)
    vault = node(d, 1110, 40, 260, 170, "device", "Vault", "HashiCorp Vault · :8200", fill=EXTERNAL_FILL)
    component(d, 1125, 120, 230, 70, "HashiCorp Vault", stereotype="subsystem", external=True)
    registry = node(d, 740, 960, 300, 150, "device", "Registry", "GHCR · OCI Distribution API", fill=EXTERNAL_FILL)
    component(d, 755, 1040, 270, 55, "OCI Registry", stereotype="subsystem", external=True)

    targets = []
    for y, name, tech, sub in [(330, "Target Cluster", "Kubernetes · :6443", "Target Cluster"),
                               (510, "Compose VM", "Docker Compose v2 · sshd :22", "Compose Host"),
                               (690, "Managed Hosts", "sshd :22", "Managed Hosts")]:
        targets.append(node(d, 1340, y, 280, 140, "device", name, tech, fill=EXTERNAL_FILL))
        component(d, 1355, y + 75, 250, 55, sub, stereotype="subsystem", external=True)

    app_box, job_box = (480, 430, 330, 210), (850, 430, 330, 210)
    # browser and identity provider
    d.edge(browser, app, LINE, exit=(1, round(130 / 160, 4)), entry=(0, frac(app_box, 460)))
    d.text(342, 412, 98, 44, "<b>Delivery<br/>Service</b><br/>HTTPS :443", align="center", size=11)
    d.edge(browser, keycloak, LINE, exit=(0.5, 1), entry=(0.5, 0))
    d.text(198, 506, 140, 60, "<b>Authentication<br/>Service</b><br/>HTTPS · OIDC", size=11)
    d.edge(app, keycloak, LINE, exit=(0, frac(app_box, 610)), entry=(1, round(30 / 150, 4)))
    d.text(342, 566, 98, 44, "<b>Authentication<br/>Service</b> · HTTPS", align="center", size=11)
    d.edge(app, postgres, LINE, exit=(0, frac(app_box, 630)), entry=(1, 0.5), points=[(400, 630), (400, 875)])
    d.text(190, 740, 206, 50, "<b>Persistence Service</b><br/>TCP :5432", align="right", size=11)
    d.edge(app, apiserver, LINE, exit=(round(60 / 330, 4), 1), entry=(0.25, 0))
    d.text(548, 666, 170, 44, "<b>Workload Service</b><br/>HTTPS :6443", size=11)
    # GitHub, registry, Vault
    d.edge(app, github, LINE, exit=(round(300 / 330, 4), 0), entry=(round(80 / 360, 4), 1))
    d.text(536, 214, 236, 80, "<b>Webhook, Repository Content,<br/>Deployment Status Service</b><br/>HTTPS :443",
           align="right", size=11)
    d.edge(job, github, LINE, exit=(round(50 / 330, 4), 0), entry=(round(200 / 360, 4), 1))
    d.text(906, 222, 196, 60, "<b>Repository Content<br/>Service</b><br/>SSH :22 (git)", size=11)
    d.edge(job, vault, LINE, exit=(round(300 / 330, 4), 0), entry=(round(40 / 260, 4), 1))
    d.text(1156, 236, 150, 44, "<b>Secret Service</b><br/>HTTPS :8200", size=11)
    d.edge(runner, github, LINE, exit=(1, round(90 / 170, 4)), entry=(0, round(90 / 170, 4)))
    d.text(542, 86, 156, 44, "<b>Artifact Upload<br/>Service</b> · HTTPS", align="center", size=11)
    d.edge(app, registry, LINE, exit=(round(280 / 330, 4), 1), entry=(round(20 / 300, 4), 0))
    d.edge(job, registry, LINE, exit=(round(50 / 330, 4), 1), entry=(round(160 / 300, 4), 0))
    d.text(764, 906, 132, 50, "<b>Registry Service</b><br/>HTTPS (OCI)", align="center", size=11)
    # targets
    d.edge(job, targets[0], LINE, exit=(1, frac(job_box, 460)), entry=(0, 0.5), points=[(1270, 460), (1270, 400)])
    d.text(1222, 344, 116, 50, "<b>Release Service</b><br/>HTTPS :6443", align="center", size=11)
    d.edge(job, targets[1], LINE, exit=(1, frac(job_box, 560)), entry=(0, frac((1340, 510, 280, 140), 560)))
    d.text(1222, 512, 116, 46, "<b>Compose Host<br/>Service</b> · SSH", align="center", size=11)
    d.edge(job, targets[2], LINE, exit=(1, frac(job_box, 620)), entry=(0, 0.5), points=[(1270, 620), (1270, 760)])
    d.text(1222, 768, 116, 46, "<b>Managed Host<br/>Service</b> · SSH", align="center", size=11)

    legend(d, 1340, 880, 280, [("subsystem", "Own subsystem"), ("external", "External system"),
                               ("namespace", "Kubernetes namespace"), ("technology", "Technology, version, port")])
    d.save()


# ---------------------------------------------------------------- service specs

def service_specs():
    service_spec("services/delivery-service", "Delivery Service", "Delivery API", [
        "me(): User",
        "projects(): ProjectView [*]",
        "artifacts(project: String, source: Source [0..1], includeClosed: Boolean): ArtifactView [*]",
        "target(id: String): TargetView",
        "plan(target: String, artifact: Integer): DeployPlan",
        "deploy(target: String, artifact: Integer, confirm: Boolean, override: Boolean): Deployment",
        "releaseLock(target: String)",
        "setMaintenance(target: String, enabled: Boolean, reason: String)",
        "deployment(id: Integer): Deployment",
        "streamLog(deployment: Integer): LogEvent [*]",
        "tearDown(preview: Integer)",
        "auditLog(before: Integer [0..1]): AuditEntry [*]",
    ], [
        ("DeployPlan", ["status: TargetStatus", "reasons: String [*]", "diff: String", "needsOverride: Boolean",
                        "lockHolder: String [0..1]", "lockUntil: DateTime [0..1]", "permissions: Permissions"]),
        ("Permissions", ["deploy: Boolean", "override: Boolean", "maintenance: Boolean"]),
        ("LogEvent", ["kind: log | status | end", "data: String"]),
    ])
    service_spec("services/webhook-service", "Webhook Service", "Artifact Intake", [
        "workflowRunCompleted(run: WorkflowRun, signature: String)",
        "pullRequestClosed(repository: String, number: Integer, signature: String)",
    ], [
        ("WorkflowRun", ["repository: String", "headRepository: String", "commit: String", "event: String",
                         "headBranch: String", "pullRequests: Integer [*]", "conclusion: String",
                         "triggeringActor: String"]),
    ])
    service_spec("services/delivery-rules-service", "Delivery Rules Service", "Delivery Rules", [
        "readiness(target: Target, artifact: Artifact): String [*]",
        "plan(user: User, target: Target, artifact: Artifact): DeployPlan",
        "deploy(user: User, request: DeployRequest): Deployment",
        "autoDeploy(artifact: Artifact)",
        "processWaitingPlaces()",
        "releaseLock(user: User, target: Target)",
        "setMaintenance(user: User, target: Target, enabled: Boolean, reason: String)",
        "tearDownPreview(user: User [0..1], preview: Preview)",
        "pullRequestClosed(project: Project, number: Integer)",
        "jobSpec(target: Target, artifact: Artifact [0..1], deployment: Deployment): JobSpec",
    ], [
        ("DeployRequest", ["target: Target", "artifact: Artifact", "confirm: Boolean", "override: Boolean"]),
        ("JobSpec", ["kind: DeploymentKind", "targetKind: TargetKind", "secretPaths: String [*]",
                     "values: Map [0..1]", "composeFile: String [0..1]"]),
    ])


def vassoc_up(d, lower, lower_box, upper, upper_box, x, lower_mult, upper_mult, style=COMPOSITION):
    """Vertical composition from a lower whole to an upper part (diamond on the whole)."""
    d.edge(lower, upper, style, exit=(round((x - lower_box[0]) / lower_box[2], 4), 0),
           entry=(round((x - upper_box[0]) / upper_box[2], 4), 1))
    d.text(x + 6, lower_box[1] - 22, 60, 20, lower_mult)
    d.text(x + 6, upper_box[1] + upper_box[3] + 2, 60, 20, upper_mult)


if __name__ == "__main__":
    object_model()
    components_server()
    components_executor()
    deployment()
    service_specs()

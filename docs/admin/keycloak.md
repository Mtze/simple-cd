---
title: Keycloak
parent: Administrators
nav_order: 2
---

# Keycloak setup
{: .no_toc }

simple-cd logs users in with OpenID Connect (authorization code flow with PKCE) and takes their roles from the client roles of its own Keycloak client.

1. TOC
{:toc}

## Client

Create an OpenID Connect client in your realm, for example with the client ID `simple-cd`:

| Setting | Value |
|---|---|
| Client ID | `simple-cd` (the same as `oidc.clientID` / `SIMPLECD_OIDC_CLIENT_ID`) |
| Client authentication | On (confidential client). Put the secret into the Secret key `oidc-client-secret`. |
| Standard flow | On |
| Direct access grants | Off |
| Valid redirect URIs | `https://cd.example.org/auth/callback` |
| Valid post logout redirect URIs | `https://cd.example.org/` |
| Web origins | not needed, the browser never calls Keycloak with CORS |
| Advanced > Proof Key for Code Exchange Code Challenge Method | `S256` |

simple-cd requests the scopes `openid`, `profile` and `email` and reads `preferred_username` (or `sub`), `name` and `email` from the ID token.
Logging out ends the simple-cd session and sends the browser to Keycloak's end-session endpoint with `id_token_hint` and `post_logout_redirect_uri`, so the Keycloak session ends too.

The issuer URL is the realm URL, for example `https://keycloak.example.org/realms/main`.
simple-cd fetches the discovery document at start and refuses to start when Keycloak is unreachable.

## Roles

All roles are **client roles of the simple-cd client**:

| Role | Grants |
|---|---|
| `project:<owner>/<repo>:developer` | deploy to available targets of the project, start and tear down previews, release own locks |
| `project:<owner>/<repo>:maintainer` | everything a developer can, plus take over locks and switch maintenance |
| any role named in a target's `role`, by convention `deploy:<target-id>` | needed in addition to the project role for that target |
| `platform-admin` | everything in every project, the audit log, orphaned previews and the config error banner |
| `viewer` (by convention) | read access only: any client role lets a user log in and see every project |

Write `<owner>/<repo>` in **lower case**.
simple-cd lowercases the project name before it compares, because GitHub names are case-insensitive.
A role `project:Example/App:developer` never matches.

The full list of what each role can do is the [permission matrix](../maintainers/index.md#permissions).
Users without any client role of simple-cd cannot log in: they get "your account has no access".
Give people who should only watch a role such as `viewer`, which grants nothing beyond logging in.
The usernames `system`, `auto-deploy` and `github` are refused, because the audit log uses them for the server's own actions.

Assign roles to groups rather than to single users where you can.
simple-cd stores the roles in the session at login, so role changes apply at the next login (sessions last 4 hours by default).
To take access away at once, remove the role and delete the user's sessions in Postgres: `DELETE FROM sessions WHERE username = '<name>';`.

## Protocol mapper (required)

Keycloak puts client roles into the **access token** by default, but simple-cd reads them from the **ID token**, from the claim `resource_access.<client-id>.roles`.
Without this mapper every user ends up without roles.

Add a mapper to the client (**Client scopes > simple-cd-dedicated > Add mapper > By configuration > User Client Role**):

| Setting | Value |
|---|---|
| Name | `client roles in ID token` |
| Client ID | `simple-cd` |
| Multivalued | On |
| Token Claim Name | `resource_access.${client_id}.roles` |
| Claim JSON Type | `String` |
| Add to ID token | On |
| Add to access token | On |
| Add to userinfo | Off |

The same mapper as JSON, as used by the development realm in `dev/keycloak-realm.json`:

```json
{
  "name": "client roles in ID token",
  "protocol": "openid-connect",
  "protocolMapper": "oidc-usermodel-client-role-mapper",
  "config": {
    "usermodel.clientRoleMapping.clientId": "simple-cd",
    "claim.name": "resource_access.${client_id}.roles",
    "multivalued": "true",
    "jsonType.label": "String",
    "id.token.claim": "true",
    "access.token.claim": "true",
    "userinfo.token.claim": "false"
  }
}
```

To check it, log in to simple-cd and open the user menu: it lists your roles.

## Create project roles

simple-cd does not create Keycloak roles itself, because that would need admin rights on the whole realm.
Instead it lists the roles the configuration uses: open **Admin > Roles** in the UI as a platform admin.
**Download Keycloak import** gives a file for **Realm settings > Action > Partial import**, and **Open realm settings in Keycloak** opens that page in the admin console of your realm.
The link uses the realm's own console (`/admin/<realm>/console/`), so an admin who only has an account in the master realm opens `/admin/master/console/#/<realm>/realm-settings` instead.
In the import dialog, tick **Client roles** and set **If a resource already exists** to **Skip**.
The dialog defaults to **Fail import** and ignores the setting in the file, so with the default the import stops at the first role that exists.
With **Skip**, existing roles and their assignments stay as they are, so import the file again after every new project or target role.
Which users or groups get a role stays your decision in Keycloak; simple-cd does not expect one group per project.

To create roles from a script instead, this one creates the two roles of a project with `kcadm.sh` and optionally assigns them to groups:

```sh
#!/bin/sh
# Usage: create-project-roles.sh <owner/repo> [developer-group] [maintainer-group]
# Needs kcadm.sh from the Keycloak distribution on the PATH.
set -eu
KEYCLOAK_URL=${KEYCLOAK_URL:-https://keycloak.example.org}
REALM=${REALM:-main}
CLIENT=${CLIENT:-simple-cd}
PROJECT=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')

# Asks for the password of the admin user.
kcadm.sh config credentials --server "$KEYCLOAK_URL" --realm master --user "${KEYCLOAK_ADMIN:-admin}"

CID=$(kcadm.sh get clients -r "$REALM" -q clientId="$CLIENT" --fields id --format csv --noquotes)
for role in developer maintainer; do
  kcadm.sh create "clients/$CID/roles" -r "$REALM" -s "name=project:$PROJECT:$role" \
    || echo "role project:$PROJECT:$role exists already"
done

if [ -n "${2:-}" ]; then
  kcadm.sh add-roles -r "$REALM" --gname "$2" --cclientid "$CLIENT" --rolename "project:$PROJECT:developer"
fi
if [ -n "${3:-}" ]; then
  kcadm.sh add-roles -r "$REALM" --gname "$3" --cclientid "$CLIENT" --rolename "project:$PROJECT:maintainer"
fi
```

```sh
./create-project-roles.sh example/app app-developers app-maintainers
```

Create target roles such as `deploy:app-prod` and the `platform-admin` role the same way with `kcadm.sh create "clients/$CID/roles" -r "$REALM" -s name=deploy:app-prod`.

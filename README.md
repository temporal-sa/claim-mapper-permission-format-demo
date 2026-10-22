# claim-mapper-permission-format-demo

**Temporal claim mapper: permission formats without ":"**

This demo shows that Temporal's **built-in** JWT claim mapper accepts permission
claim values in formats other than `<namespace>:<role>`, such as
`<namespace>_<role>`, `<namespace>-<role>` or `<namespace>_ROLE_<role>`.

It's a configuration setting, `permissionsRegex`. You don't need a custom claim
mapper, a plugin, or a custom server build.

It also shows the second thing you need for the **Temporal UI** to load: a
system-level role (`temporal-system`) in the token. Namespace roles alone,
even `admin`, get a 403 on `GET /api/v1/namespaces`.

The repo has two parts:

| Part | What it runs | Needs |
|---|---|---|
| [Go demo](#part-1-go-demo-go-run-) (`go run .`) | The real claim mapper, authorizer and config loader from `go.temporal.io/server` v1.32.1, in-process | Go |
| [Try it in the UI](#part-2-try-it-in-the-temporal-ui-e2e) (`e2e/`) | Keycloak + Temporal server 1.32.1 + Temporal UI 2.53.3 in Docker, logging in through the browser | Docker, `curl`, `jq` |

## The short answer

For a token with `"roles": ["xyz-deployment_ROLE_write", ...]`, the server needs:

```yaml
global:
  authorization:
    authorizer: default
    claimMapper: default
    permissionsClaimName: roles
    permissionsRegex: '^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$'
```

And for the UI to load, the user's **access token** must also contain a
system-level role, for example `temporal-system_ROLE_read`. Read the
[caveat](#caveats) about what that role grants before you roll it out.

Two mistakes that are easy to make, and fail silently:

- **The key is `permissionsRegex`:** lowercase `p`, plural `permissions`.
  `PermissionsRegex` and `permissionRegex` are ignored without any error or
  warning (scenario H).
- **The regex must have two named groups, `namespace` and `role`.** A regex
  like `_ROLE_` is thrown away with one warning in the server log, and the
  server goes back to `<namespace>:<role>` parsing, so nothing maps
  (scenario G).

---

## Part 1: Go demo (`go run .`)

**It runs the real server code, not a re-implementation.** `main.go` imports
`go.temporal.io/server` v1.32.1 and calls the same functions the Temporal
server uses for every incoming request:

```go
authorization.NewDefaultJWTClaimMapper(keyProvider, cfg, logger) // token -> claims
authorization.NewDefaultAuthorizer()                             // claims + API -> allow/deny
config.Load(config.WithConfigFile(path))                         // YAML -> config (scenario H)
```

`cfg` is `config.Authorization`, the Go struct the server fills in from the
`global.authorization` section of its YAML config. Its Go field is named
`PermissionsRegex` (Go fields are capitalized), but the YAML key is
`permissionsRegex`.

For each scenario, the demo:

1. Creates a JWT with a permissions claim in the format being tested, and
   signs it with a throwaway RSA key (with a `kid` header and a `sub` claim,
   which the default mapper requires).
2. Passes it to the claim mapper as a `Bearer` token, exactly as a client SDK
   would send it.
3. Prints the server's own warning log lines, and the namespace and role
   grants the mapper produced.
4. For E, F and G, asks the default authorizer whether those grants allow the
   calls the UI makes.

**What it does not do:** it doesn't start a Temporal server. Key retrieval from
a JWKS URL is replaced by a static in-memory key. Part 2 runs the full thing.

### Prerequisites

- Go **1.26.8 or newer**, which the v1.32.1 server module requires. If your Go is
  older, Go downloads 1.26.8 automatically the first time you build.
- Network access the first time you run it, to download modules.

### Run

```bash
go run .
```

### Scenarios

| | Config | Token | Result |
|---|---|---|---|
| **A** | default (no regex) | `permissions`: `payments:write`, … | ✅ maps: the standard format, as a baseline |
| **B** | default (no regex) | `permissions`: `payments_write`, … | ❌ **nothing maps** |
| **C** | `permissionsRegex` for `_` | `permissions`: `payments_write`, `payments_prod_admin`, … | ✅ maps, same grants as A plus `payments_prod` |
| **D** | `permissionsRegex` for `-` | `permissions`: `payments-write`, … | ✅ maps, same as C |
| **E** | `permissionsRegex` for `_ROLE_` | `roles`: `xyz-deployment_ROLE_*` only | ✅ maps, but ❌ **`ListNamespaces` / `GetClusterInfo` denied** (UI 403) |
| **F** | same as E | E + `temporal-system_ROLE_read` | ✅ UI calls allowed, and ⚠️ read access to **every** namespace |
| **G** | `permissionsRegex: _ROLE_` (no named groups) | same as F | ❌ regex discarded with a warning, nothing maps |
| **H** | YAML with `permissionsRegex` / `PermissionsRegex` / `permissionRegex` | n/a | only the first spelling is loaded |

Actual output:

```
=== A. Default config, colon format (baseline) ===
permissionsRegex : ""
token permissions: [temporal-system:read payments:write orders:read]
RESULT: subject=demo-service-account system=read
        namespace orders             -> read
        namespace payments           -> write

=== B. Default config, underscore format (the problem) ===
permissionsRegex : ""
token permissions: [temporal-system_read payments_write payments_prod_admin orders_read billing_viewer]
server log WARN : ignoring permission in unexpected format: temporal-system_read
server log WARN : ignoring permission in unexpected format: payments_write
server log WARN : ignoring permission in unexpected format: payments_prod_admin
server log WARN : ignoring permission in unexpected format: orders_read
server log WARN : ignoring permission in unexpected format: billing_viewer
RESULT: subject=demo-service-account system=none
        namespaces: (none mapped)

=== C. permissionsRegex, underscore separator ===
permissionsRegex : "^(?P<namespace>.+)_(?P<role>read|write|worker|admin)$"
token permissions: [temporal-system_read payments_write payments_prod_admin orders_read billing_viewer]
server log WARN : ignoring permission not matching pattern: billing_viewer
RESULT: subject=demo-service-account system=read
        namespace orders             -> read
        namespace payments           -> write
        namespace payments_prod      -> admin

=== D. permissionsRegex, hyphen separator ===
permissionsRegex : "^(?P<namespace>.+)-(?P<role>read|write|worker|admin)$"
token permissions: [temporal-system-read payments-write payments-prod-admin orders-read billing-viewer]
server log WARN : ignoring permission not matching pattern: billing-viewer
RESULT: subject=demo-service-account system=read
        namespace orders             -> read
        namespace payments           -> write
        namespace payments-prod      -> admin

=== E. _ROLE_ regex, namespace roles only (UI gets 403) ===
permissionsRegex : "^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$"
token roles      : [xyz-deployment_ROLE_read xyz-deployment_ROLE_write xyz-deployment_ROLE_admin]
RESULT: subject=demo-service-account system=none
        namespace xyz-deployment     -> admin+write+read
AUTHORIZER:
        DENY  ListNamespaces               (cluster)
        DENY  GetClusterInfo               (cluster)
        ALLOW ListWorkflowExecutions       (namespace xyz-deployment)
        DENY  ListWorkflowExecutions       (namespace other-team)
        DENY  GetWorkflowExecutionHistory  (namespace other-team)
        DENY  StartWorkflowExecution       (namespace other-team)

=== F. _ROLE_ regex, plus temporal-system_ROLE_read (UI loads) ===
permissionsRegex : "^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$"
token roles      : [xyz-deployment_ROLE_read xyz-deployment_ROLE_write xyz-deployment_ROLE_admin temporal-system_ROLE_read]
RESULT: subject=demo-service-account system=read
        namespace xyz-deployment     -> admin+write+read
AUTHORIZER:
        ALLOW ListNamespaces               (cluster)
        ALLOW GetClusterInfo               (cluster)
        ALLOW ListWorkflowExecutions       (namespace xyz-deployment)
        ALLOW ListWorkflowExecutions       (namespace other-team)
        ALLOW GetWorkflowExecutionHistory  (namespace other-team)
        DENY  StartWorkflowExecution       (namespace other-team)

=== G. permissionsRegex without named groups (silent fallback) ===
permissionsRegex : "_ROLE_"
token roles      : [xyz-deployment_ROLE_read xyz-deployment_ROLE_write xyz-deployment_ROLE_admin temporal-system_ROLE_read]
server log WARN : permissions regex does not have namespace or role named group
server log WARN : ignoring permission in unexpected format: xyz-deployment_ROLE_read
server log WARN : ignoring permission in unexpected format: xyz-deployment_ROLE_write
server log WARN : ignoring permission in unexpected format: xyz-deployment_ROLE_admin
server log WARN : ignoring permission in unexpected format: temporal-system_ROLE_read
RESULT: subject=demo-service-account system=none
        namespaces: (none mapped)
AUTHORIZER:
        DENY  ListNamespaces               (cluster)
        DENY  GetClusterInfo               (cluster)
        DENY  ListWorkflowExecutions       (namespace xyz-deployment)
        DENY  ListWorkflowExecutions       (namespace other-team)
        DENY  GetWorkflowExecutionHistory  (namespace other-team)
        DENY  StartWorkflowExecution       (namespace other-team)

=== H. YAML key spelling, loaded by the server's config loader ===
permissionsRegex : loaded permissionsRegex = "^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$"
PermissionsRegex : loaded permissionsRegex = ""
permissionRegex  : loaded permissionsRegex = ""
```

What to notice:

- **B vs. C:** the token is identical. One config value takes the result from
  "no access" to the correct grants.
- **`payments_prod_admin` → `payments_prod` / `admin`:** namespaces that contain
  the separator still parse correctly. The role is anchored to the end of the
  string and limited to the four valid role names, so the namespace part
  matches greedily up to the last separator.
- **`billing_viewer` is ignored:** `viewer` isn't a Temporal role, so the entry
  doesn't match and is skipped with a warning. A shared identity provider can
  keep entries for other applications in the same claim.
- **E:** every role maps, and the user can work in `xyz-deployment`, but the two
  cluster-scoped calls the UI makes on every page load are denied. The
  authorizer checks only the system role for cluster-scoped APIs.
- **F:** `temporal-system_ROLE_read` fixes the UI, but it also lets the user
  read workflows and history in `other-team`, which the token says nothing
  about. It doesn't grant writes there.
- **G:** the customer's original `_ROLE_` regex. The server keeps running; the only
  signal is one warning at startup, followed by a warning per ignored role.
- **H:** the server's config loader silently ignores unknown keys. There is no
  warning at all for a misspelled key; the server simply behaves as if no regex
  was set.

---

## Part 2: Try it in the Temporal UI (`e2e/`)

This part runs a real Temporal server, the Temporal UI and a login server
(Keycloak) on your machine in Docker. You log in as two test users and see the
problem and the fix in the browser. Everything uses stock images, configured
only through environment variables; there is no custom server build.

### Step 1: Before you start

- **Docker must be running.**
- **Free up the ports.** The stack uses ports 7233, 7243, 8180 and 8233. A
  `temporal server start-dev` uses 7233 and 8233 too, so stop it first.
  Docker does **not** warn you about this clash; the UI just shows a blank
  page. To check, run:

  ```bash
  lsof -nP -iTCP:8233 -sTCP:LISTEN
  ```

  Before you start the stack this should print nothing. After you start it,
  the only line should be `com.docker`.
- **Use a private (incognito) browser window** for each login. This stops an
  old login or old cached pages from getting in the way.

### Step 2: Start the stack

From the repo root:

```bash
docker compose -f e2e/compose.yaml up -d
```

Wait about a minute (longer the first time, while images download). It's ready
when this shows `temporal-setup` as `Exited (0)` and the other three as `Up`:

```bash
docker compose -f e2e/compose.yaml ps -a
```

Setup creates two namespaces, `xyz-deployment` and `other-team`, and one
workflow in `other-team` called `other-team-sample`.

### Step 3: The test users

Log in to the UI with these. Both use the password **`demo-password`**.

| Username | Roles in the token | What should happen |
|---|---|---|
| `ns-only` | `xyz-deployment_ROLE_read`, `_write`, `_admin` | ❌ UI won't load: you're sent back to the login page |
| `ns-plus-system` | the same, plus `temporal-system_ROLE_read` | ✅ UI loads |

The users are defined in
[`e2e/keycloak/temporal-realm.json`](e2e/keycloak/temporal-realm.json).

### Step 4: Run the tests in the UI

Open the UI at **http://localhost:8233**. Each test starts in a **new private
window**, so you're logged out.

#### Test A: namespace roles only, the UI is blocked (the customer's problem)

1. Open http://localhost:8233.
2. Optional: open the browser's developer tools on the **Network** tab, so you
   can see the 403.
3. Click **Continue to SSO**.
4. Log in as **`ns-only`**.

**Expected:** the login works, but you land back on the "Welcome back" page
instead of seeing workflows. In the Network tab, the requests named
`namespaces` and `cluster-info` show status **403**.

**Why:** the UI calls `ListNamespaces` and `GetClusterInfo` on every page.
Those are cluster-level calls, and the server only allows them for users with a
`temporal-system` role. Admin on a namespace isn't enough.

You can also see the 403s from the terminal:

```bash
docker compose -f e2e/compose.yaml logs --no-log-prefix temporal-ui | grep -E 'api/v1/(namespaces|cluster-info)\?' | jq -r '[.method, .uri, .status] | @tsv'
```

#### Test B: add the system role, the UI works (the fix)

1. Open a new private window and go to http://localhost:8233.
2. Click **Continue to SSO** and log in as **`ns-plus-system`**.

**Expected:** the **Workflows** page for `xyz-deployment` loads (it shows
0 workflows).

#### Test C: what the system role also gives access to (the caveat)

1. Still logged in as `ns-plus-system`, click the namespace picker at the top
   left of the page (it shows `xyz-deployment`) and choose **`other-team`**.

**Expected:** you can see the `other-team-sample` workflow, even though this
user has no role on `other-team`. `temporal-system_ROLE_read` gives read
access to **every** namespace. (It doesn't allow writes there; Step 5 shows
that.)

#### Test D: the broken regex, nobody gets in (the customer's original config)

1. Restart the server with the customer's original regex, `_ROLE_`, which has
   no named groups:

   ```bash
   PERMISSIONS_REGEX=_ROLE_ docker compose -f e2e/compose.yaml up -d
   ```

2. Open a new private window and log in as **`ns-plus-system`**.

**Expected:** you're sent back to the login page, just like `ns-only` in
Test A. The roles are in the token, but the server can't read them. The server
logs a warning when it starts:

```bash
docker compose -f e2e/compose.yaml logs temporal | grep "permissions regex"
```

```
{"level":"warn","msg":"permissions regex does not have namespace or role named group",...}
```

The server still starts normally; this warning is the only sign.

3. **Put the correct regex back** before doing anything else:

   ```bash
   docker compose -f e2e/compose.yaml up -d
   ```

### Step 5 (optional): the same checks from the command line

[`e2e/verify.sh`](e2e/verify.sh) logs in as both users and calls the Temporal
API directly, through the same permission checks the UI goes through. It needs
`curl` and `jq`.

```bash
./e2e/verify.sh
```

Output with the correct regex (`ALLOW` means the call worked, `DENY` means it
got a 403):

```
=== Server config ===
permissionsRegex (env TEMPORAL_JWT_PERMISSIONS_REGEX): '^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$'
regex warnings in server log: none

=== User: ns-only ===
access token header : {"alg":"RS256","kid":"..."}
access token claims : {"sub":"...","roles":["xyz-deployment_ROLE_write","xyz-deployment_ROLE_read","xyz-deployment_ROLE_admin"]}
  DENY  ListNamespaces               (cluster; UI calls this on every page)
  DENY  GetClusterInfo               (cluster; UI calls this on every page)
  ALLOW ListWorkflowExecutions       (namespace xyz-deployment: user has a role)
  DENY  ListWorkflowExecutions       (namespace other-team: no role in token)
  DENY  GetWorkflowExecutionHistory  (namespace other-team: no role in token)
  DENY  StartWorkflowExecution       (namespace other-team: no role in token)

=== User: ns-plus-system ===
access token header : {"alg":"RS256","kid":"..."}
access token claims : {"sub":"...","roles":["xyz-deployment_ROLE_write","xyz-deployment_ROLE_read","temporal-system_ROLE_read","xyz-deployment_ROLE_admin"]}
  ALLOW ListNamespaces               (cluster; UI calls this on every page)
  ALLOW GetClusterInfo               (cluster; UI calls this on every page)
  ALLOW ListWorkflowExecutions       (namespace xyz-deployment: user has a role)
  ALLOW ListWorkflowExecutions       (namespace other-team: no role in token)
  ALLOW GetWorkflowExecutionHistory  (namespace other-team: no role in token)
  DENY  StartWorkflowExecution       (namespace other-team: no role in token)
```

With the broken regex from Test D, every line is `DENY` for both users, and
the script prints the server's warning.

### Step 6: Stop the stack

To stop and remove everything (the namespaces and workflow are recreated the
next time you start it):

```bash
docker compose -f e2e/compose.yaml down -v
```

To stop it but keep the namespaces and workflow, leave off `-v`:

```bash
docker compose -f e2e/compose.yaml down
```

### Reference: what's running

| Service | Image | Address |
|---|---|---|
| Temporal UI | `temporalio/ui:2.53.3` | http://localhost:8233 |
| Keycloak (login server, realm `temporal`) | `quay.io/keycloak/keycloak:26.4` | http://localhost:8180 (admin console login: `admin` / `admin`) |
| Temporal server | `temporalio/server:1.32.1` | gRPC `localhost:7233`, HTTP API http://localhost:7243 |
| One-shot setup | `temporalio/admin-tools:1.32.1` | creates the namespaces and the sample workflow, then exits |

Keycloak puts the user's roles in a `roles` claim **in the access token only**
(not the ID token), which is what the Temporal server reads.

### Check which token reaches the server

The default claim mapper reads only the `Authorization` header (the access
token). The UI also sends the ID token, in an `Authorization-Extras` header,
but the default claim mapper never reads it. So the roles claim must be in the
**access token**.

To check what your UI is sending:

1. Open the UI, then the browser's developer tools, **Network** tab.
2. Reload and select the request to `/api/v1/namespaces`.
3. Under **Request Headers**, copy the value after `Authorization: Bearer `.
4. Decode its middle segment **locally**. Don't paste tokens into public
   websites; they're live credentials. For example:

   ```bash
   pbpaste | cut -d. -f2 | tr '_-' '/+' | base64 -d 2>/dev/null | jq '{sub, roles}'
   ```

   (Add one or two `=` to the end of the segment if `base64` complains about
   padding.)

If `roles` is missing there but present in the ID token, fix the IdP's claim
mapping, for example the Keycloak "realm roles" mapper's **Add to access
token** setting. Don't switch the UI to `useIdTokenAsBearer` just to get roles
through.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Login works, then the UI returns to its login page; browser shows **403 on `GET /api/v1/namespaces`** (and `/api/v1/cluster-info`) | The token has namespace roles but no `temporal-system` role. `ListNamespaces` and `GetClusterInfo` are cluster-scoped, and the default authorizer checks only the system role for those ([temporalio/temporal#11639](https://github.com/temporalio/temporal/issues/11639)). | Add a system role to the user's access token, for example `temporal-system_ROLE_read`. Read the [caveats](#caveats). |
| Roles are in the token, but nothing maps; every call is denied | `permissionsRegex` is missing its named groups (G), or doesn't match your values, or the key is misspelled (H) | Use `permissionsRegex` exactly, with `(?P<namespace>…)` and `(?P<role>…)`, single-quoted in YAML. Test it with this demo. |
| Server log shows `ignoring permission in unexpected format: …` for every role | The server is parsing `<namespace>:<role>`, so no regex is active: either it was never set, the key is misspelled, or it was discarded | Check startup logs for `permissions regex does not have namespace or role named group` or `failed to compile permissions regex`. If neither appears, the key isn't being read. |
| Server starts fine but behaves as if no regex is set | A broken regex is discarded with only a warning, and a misspelled key is ignored with no warning at all | As above. There is no setting that makes either one fail startup. |
| Roles are in the ID token but not the access token | The default claim mapper only reads the `Authorization` header (access token) | Configure the IdP to add the roles claim to the access token. See [Check which token reaches the server](#check-which-token-reaches-the-server). |
| `unexpected value type of "sub" claim` or `malformed token - no "kid" header` | The default mapper requires a string `sub` claim and a `kid` JWT header | Configure the IdP to include both (most do by default). |

## Caveats

- **`temporal-system_ROLE_read` is system-wide.** The default authorizer combines
  the system role with the namespace role for every namespace-scoped call. A
  user with system `read` can therefore list and read workflows, histories and
  other read-only data in **every** namespace, including ones their token says
  nothing about. They can't write there. Confirmed in both parts of this repo
  (`other-team`).
- **If that's not acceptable,** the alternative is a small custom `Authorizer`
  that wraps the default one (`authorization.NewDefaultAuthorizer()`) and also
  allows `ListNamespaces` and `GetClusterInfo` for any authenticated caller.
  That requires building your own server binary/image with
  `temporal.WithAuthorizer(...)`. This repo doesn't include it.
- **No upstream fix yet.** [temporalio/temporal#11881](https://github.com/temporalio/temporal/pull/11881)
  ("Allow namespace readers to call cluster-level readonly APIs") is open and
  not merged.
- **`ListNamespaces` returns all namespaces.** With the system read role the UI's
  namespace picker shows every namespace, not just the user's.

## What was confirmed, and how

| Claim | How it was confirmed |
|---|---|
| `permissionsRegex` with named groups maps `_`, `-` and `_ROLE_` formats | Ran: Go demo C, D, E; e2e stack |
| A regex without named groups is discarded with a warning; fallback to `:` parsing | Ran: Go demo G; e2e stack with `PERMISSIONS_REGEX=_ROLE_` (log message captured above) |
| A regex that fails to compile is also discarded, with `failed to compile permissions regex '…'` | **Source only** (`default_jwt_claim_mapper.go`, `NewDefaultJWTClaimMapper`); not run |
| `PermissionsRegex` and `permissionRegex` keys are silently ignored | Ran: Go demo H, with the server's own config loader. Not run against a server started from a YAML file (the e2e stack configures the server through environment variables). |
| Namespace-only tokens get 403 on `ListNamespaces` / `GetClusterInfo` | Ran: Go demo E; e2e `verify.sh`; e2e UI in the browser |
| Adding `temporal-system_ROLE_read` makes the UI load | Ran: Go demo F; e2e `verify.sh`; e2e UI in the browser |
| System read gives read, but not write, access to namespaces with no role in the token | Ran: Go demo F; e2e `verify.sh`; e2e UI (`other-team` workflow visible) |
| The default mapper reads only the `Authorization` header, not `Authorization-Extras` | Source (`GetClaims` uses `AuthInfo.AuthToken`). In the e2e stack the ID token has no `roles` and the UI still works, so the server must be using the access token. |
| The default mapper requires a `kid` header and a string `sub` claim | Source (`parseJWTWithAudience`, `GetClaims`); the Go demo sets both |
| `authorization.NewDefaultAuthorizer()` takes no arguments | Ran: Go demo E, F, G call it |
| UI 2.53.3 sends the ID token in `Authorization-Extras` | UI server log: `Validating ID token from Authorization-Extras header`. The Network-tab steps above weren't run as written. |
| The UI's 403 behavior on server 1.31.2 | **From [#11639](https://github.com/temporalio/temporal/issues/11639)**; this repo tested only 1.32.1. The authorizer code involved is the same. |

## Version requirements

| Component | Requirement |
|---|---|
| Temporal server (self-hosted) | **v1.29.0 or newer**. `permissionsRegex` was added in [temporalio/temporal#7574](https://github.com/temporalio/temporal/pull/7574). Earlier versions only support `<namespace>:<role>`. |
| Go demo | Go 1.26.8+; server module v1.32.1 |
| e2e stack | Docker with Compose v2, `curl`, `jq`; images `temporalio/server:1.32.1`, `temporalio/ui:2.53.3`, `temporalio/admin-tools:1.32.1`, `quay.io/keycloak/keycloak:26.4` |

This applies to the open-source server's default claim mapper and authorizer.
Temporal Cloud uses its own authentication and authorization and isn't
configured this way.

`permissionsRegex` isn't in the self-hosted security guide yet, which documents
only `<namespace>:<role>`. It is documented in the Go package docs for
[`go.temporal.io/server/common/config`](https://pkg.go.dev/go.temporal.io/server/common/config)
and in PR #7574.

## Source references (server v1.32.1)

- Config field: [`common/config/config.go`](https://github.com/temporalio/temporal/blob/v1.32.1/common/config/config.go) (`Authorization.PermissionsRegex`, YAML key `permissionsRegex`)
- Regex compilation and fallback: [`common/authorization/default_jwt_claim_mapper.go`](https://github.com/temporalio/temporal/blob/v1.32.1/common/authorization/default_jwt_claim_mapper.go) (`NewDefaultJWTClaimMapper`)
- Per-permission parsing and the `temporal-system` scope: same file, `extractPermissions`
- Cluster vs. namespace scope: [`common/authorization/default_authorizer.go`](https://github.com/temporalio/temporal/blob/v1.32.1/common/authorization/default_authorizer.go) (`Authorize`)
- API scopes (`ListNamespaces`, `GetClusterInfo` are `ScopeCluster`): [`common/api/metadata.go`](https://github.com/temporalio/temporal/blob/v1.32.1/common/api/metadata.go)
- UI 403 issue: [temporalio/temporal#11639](https://github.com/temporalio/temporal/issues/11639)

## Files

| File | Purpose |
|---|---|
| `main.go` | Go demo: signs test tokens, runs them through the real claim mapper, authorizer and config loader |
| `go.mod`, `go.sum` | Pinned dependencies (`go.temporal.io/server` v1.32.1) |
| `e2e/compose.yaml` | Keycloak + Temporal server + UI + setup |
| `e2e/keycloak/temporal-realm.json` | Realm with roles, test users, and the `roles` access-token mapper |
| `e2e/setup.sh` | Creates namespaces and a sample workflow with an admin service-account token |
| `e2e/verify.sh` | Calls the server as each test user and prints allow/deny |
| `e2e/dynamicconfig/docker.yaml` | Empty dynamic config file, required by the server image |
| `README.md` | This document |

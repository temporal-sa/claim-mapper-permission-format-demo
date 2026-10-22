# Go demo (`go run .`)

[← Back to README](../README.md)

**It runs the real server code, not a re-implementation.** [`main.go`](../main.go) imports
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
a JWKS URL is replaced by a static in-memory key. The
[UI walkthrough](ui-walkthrough.md) runs the full thing.

## Prerequisites

- Go **1.26.8 or newer**, which the v1.32.1 server module requires. If your Go is
  older, Go downloads 1.26.8 automatically the first time you build.
- Network access the first time you run it, to download modules.

## Run

```bash
go run .
```

## Tests

[`main_test.go`](../main_test.go) turns scenarios E–H into assertions, using
the same real server code, so a server upgrade that changes the behaviour
fails loudly instead of just printing different output:

```bash
go test ./...
```

They also run on every pull request, through
[`.github/workflows/test.yml`](../.github/workflows/test.yml).

## Scenarios

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

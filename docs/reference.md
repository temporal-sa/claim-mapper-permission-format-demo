# Reference

[← Back to README](../README.md)

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Login works, then the UI returns to its login page; browser shows **403 on `GET /api/v1/namespaces`** (and `/api/v1/cluster-info`) | The token has namespace roles but no `temporal-system` role. `ListNamespaces` and `GetClusterInfo` are cluster-scoped, and the default authorizer checks only the system role for those ([temporalio/temporal#11639](https://github.com/temporalio/temporal/issues/11639)). | Add a system role to the user's access token, for example `temporal-system_ROLE_read`. Read the [caveats](#caveats). |
| Roles are in the token, but nothing maps; every call is denied | `permissionsRegex` is missing its named groups ([scenario G](go-demo.md)), or doesn't match your values, or the key is misspelled ([scenario H](go-demo.md)) | Use `permissionsRegex` exactly, with `(?P<namespace>…)` and `(?P<role>…)`, single-quoted in YAML. Test it with the [Go demo](go-demo.md). |
| Server log shows `ignoring permission in unexpected format: …` for every role | The server is parsing `<namespace>:<role>`, so no regex is active: either it was never set, the key is misspelled, or it was discarded | Check startup logs for `permissions regex does not have namespace or role named group` or `failed to compile permissions regex`. If neither appears, the key isn't being read. |
| Server starts fine but behaves as if no regex is set | A broken regex is discarded with only a warning, and a misspelled key is ignored with no warning at all | As above. There is no setting that makes either one fail startup. |
| Roles are in the ID token but not the access token | The default claim mapper only reads the `Authorization` header (access token) | Configure the IdP to add the roles claim to the access token. See [Inspecting tokens and requests in developer tools](ui-walkthrough.md#inspecting-tokens-and-requests-in-developer-tools). |
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
| A regex without named groups is discarded with a warning; fallback to `:` parsing | Ran: Go demo G; e2e stack with `PERMISSIONS_REGEX=_ROLE_` (log message in the [UI walkthrough](ui-walkthrough.md), Test D) |
| A regex that fails to compile is also discarded, with `failed to compile permissions regex '…'` | **Source only** (`default_jwt_claim_mapper.go`, `NewDefaultJWTClaimMapper`); not run |
| `PermissionsRegex` and `permissionRegex` keys are silently ignored | Ran: Go demo H, with the server's own config loader. Not run against a server started from a YAML file (the e2e stack configures the server through environment variables). |
| Namespace-only tokens get 403 on `ListNamespaces` / `GetClusterInfo` | Ran: Go demo E; e2e `verify.sh`; e2e UI in the browser |
| Adding `temporal-system_ROLE_read` makes the UI load | Ran: Go demo F; e2e `verify.sh`; e2e UI in the browser |
| System read gives read, but not write, access to namespaces with no role in the token | Ran: Go demo F; e2e `verify.sh`; e2e UI (`other-team` workflow visible) |
| The default mapper reads only the `Authorization` header, not `Authorization-Extras` | Source (`GetClaims` uses `AuthInfo.AuthToken`). In the e2e stack the ID token has no `roles` and the UI still works, so the server must be using the access token. |
| The default mapper requires a `kid` header and a string `sub` claim | Source (`parseJWTWithAudience`, `GetClaims`); the Go demo sets both |
| `authorization.NewDefaultAuthorizer()` takes no arguments | Ran: Go demo E, F, G call it |
| UI 2.53.3 sends the ID token in `Authorization-Extras` | UI server log: `Validating ID token from Authorization-Extras header`. Not yet checked in the Network tab ([steps](ui-walkthrough.md#which-header-carries-which-token)). |
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
| [`main.go`](../main.go) | Go demo: signs test tokens, runs them through the real claim mapper, authorizer and config loader |
| [`main_test.go`](../main_test.go) | Tests asserting Go demo scenarios E–H (`go test ./...`) |
| [`.github/workflows/test.yml`](../.github/workflows/test.yml) | Runs `go vet` and `go test` on pull requests and pushes to `main` |
| `go.mod`, `go.sum` | Pinned dependencies (`go.temporal.io/server` v1.32.1) |
| `e2e/compose.yaml` | Keycloak + Temporal server + UI + setup |
| `e2e/keycloak/temporal-realm.json` | Realm with roles, test users, and the `roles` access-token mapper |
| `e2e/setup.sh` | Creates namespaces and a sample workflow with an admin service-account token |
| `e2e/verify.sh` | Calls the server as each test user and prints allow/deny |
| `e2e/dynamicconfig/docker.yaml` | Empty dynamic config file, required by the server image |
| `README.md` | Overview: the problem, the fix, and a 5-minute check in the UI |
| `docs/ui-walkthrough.md` | Full UI walkthrough (Tests A–D), `verify.sh`, inspecting tokens and requests in developer tools |
| `docs/go-demo.md` | Go demo scenarios A–H and their output |
| `docs/reference.md` | This document |

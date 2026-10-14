# claim-mapper-permission-format-demo

**Temporal claim mapper: permission formats without ":"**

This demo shows that Temporal's **built-in** JWT claim mapper accepts permission
claim values in formats other than `<namespace>:<role>`, such as
`<namespace>_<role>` or `<namespace>-<role>`.

It's a configuration setting, `permissionsRegex`. You don't need a custom claim
mapper, a plugin, or a custom server build.

## The question this answers

> The claim *name* is configurable, but the claim *value* format is hardcoded to
> `<namespace>:<rolename>`. Our organization doesn't allow ":" in claim values.
> Can the separator be made configurable, e.g. `<namespace>_<rolename>` or
> `<namespace>-<rolename>`?

**Answer:** yes, and it already is. Since server **v1.29.0**, the default claim
mapper accepts a regular expression (`global.authorization.permissionsRegex`)
that defines how to pull the namespace and role out of each permission value.
Any separator works, or none, and the order of namespace and role can even be
reversed.

## What the demo runs (and what it doesn't)

**It runs the real server code, not a re-implementation.** `main.go` imports
`go.temporal.io/server` v1.32.1 and calls the same function the Temporal server
uses to process every incoming token:

```go
authorization.NewDefaultJWTClaimMapper(keyProvider, cfg, logger)
```

`cfg` is `config.Authorization`, the same Go struct the server fills in from
the `global.authorization` section of its YAML config. Setting
`PermissionsRegex` in the demo is exactly what setting `permissionsRegex:` in
the YAML does.

For each scenario, the demo:

1. Creates a JWT with a `permissions` claim in the format being tested, and
   signs it with a throwaway RSA key.
2. Passes it to the claim mapper as a `Bearer` token, exactly as a client SDK
   would send it.
3. Prints the namespace and role grants the mapper produced. These grants are
   what Temporal's authorizer uses to allow or deny every API call.

**Why this is enough to prove the point.** The claim mapper is the only part
of the server that reads the permission string. It turns `payments_write` into
"namespace `payments` → role `write`". After that step the authorizer only sees
the result, never the original string, so the separator has no effect on
anything downstream. If two formats produce the same grants (compare scenarios A
and C below), the server treats them the same.

**What it does not do:** it doesn't start a Temporal server or make API calls
against one. Key retrieval from a JWKS URL is replaced by a static in-memory
key, the one part of the token pipeline that is stubbed. Neither affects how
permission values are parsed.

## Prerequisites

- Go **1.26.8 or newer**, which the v1.32.1 server module requires. If your Go is
  older, Go downloads 1.26.8 automatically the first time you build.
- Network access the first time you run it, to download modules.

## Run

```bash
go run .
```

`go.mod` and `go.sum` are included, so no other setup is needed.

## Scenarios and expected output

The token in each scenario holds the same five grants written in the format
being tested: `temporal-system` read (cluster-wide), `payments` write,
`payments_prod` admin, `orders` read, plus one entry for an unrelated
application (`billing_viewer`). Scenario A uses the plain colon format and
leaves out `payments_prod` and `billing`.

| | Config | Token permissions | Result |
|---|---|---|---|
| **A** | default (no regex) | `payments:write`, `orders:read`, … | ✅ maps: the standard format, as a baseline |
| **B** | default (no regex) | `payments_write`, `orders_read`, … | ❌ **nothing maps**: the problem described above |
| **C** | `permissionsRegex` for `_` | `payments_write`, `payments_prod_admin`, … | ✅ maps, same grants as A plus `payments_prod` |
| **D** | `permissionsRegex` for `-` | `payments-write`, `payments-prod-admin`, … | ✅ maps, same as C |

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
RESULT: subject=demo-service-account system=none
        namespaces: (none mapped)

=== C. permissionsRegex, underscore separator ===
permissionsRegex : "^(?P<namespace>.+)_(?P<role>read|write|worker|admin)$"
token permissions: [temporal-system_read payments_write payments_prod_admin orders_read billing_viewer]
RESULT: subject=demo-service-account system=read
        namespace orders             -> read
        namespace payments           -> write
        namespace payments_prod      -> admin

=== D. permissionsRegex, hyphen separator ===
permissionsRegex : "^(?P<namespace>.+)-(?P<role>read|write|worker|admin)$"
token permissions: [temporal-system-read payments-write payments-prod-admin orders-read billing-viewer]
RESULT: subject=demo-service-account system=read
        namespace orders             -> read
        namespace payments           -> write
        namespace payments-prod      -> admin
```

What to notice:

- **B vs. C:** the token is identical. The only change is one config value, and
  it takes the result from "no access" to the correct grants.
- **A vs. C:** `payments` and `orders` get the same roles from either format.
  Downstream of the mapper, the two are the same.
- **`payments_prod_admin` → `payments_prod` / `admin`:** namespaces that contain
  the separator still parse correctly. The role is anchored to the end of the
  string and limited to the four valid role names, so the namespace part
  matches greedily up to the last separator. A simple "split on `_`" setting
  couldn't do this.
- **`billing_viewer` is ignored:** `viewer` isn't a Temporal role, so the entry
  doesn't match and is skipped. This lets a shared identity provider keep
  entries for other applications in the same claim.
- **`temporal-system_read` → `system=read`:** cluster-wide permissions use the
  same format, with the reserved namespace `temporal-system`.

## Server configuration

The setting for scenario C in the server's YAML config:

```yaml
global:
  authorization:
    authorizer: default
    claimMapper: default
    permissionsClaimName: permissions      # name of the JWT claim to read
    permissionsRegex: '^(?P<namespace>.+)_(?P<role>read|write|worker|admin)$'
    jwtKeyProvider:
      keySourceURIs:
        - https://idp.example.com/.well-known/jwks.json
      refreshInterval: 1m
```

For `-`, replace `_` in the regex with `-`.

### How to write the regex

- It must contain two named groups, `(?P<namespace>...)` and `(?P<role>...)`.
  Their order in the string doesn't matter.
- Valid roles are `read`, `write`, `worker`, and `admin`. Listing them
  explicitly, as above, is recommended: namespaces can then contain the
  separator, and entries with unknown roles are skipped instead of misparsed.
- Anchor it with `^` and `$` so that only whole values match.
- Each permission value is matched separately. Values that don't match are
  ignored and a warning is logged.

### ⚠️ A broken regex fails quietly

If the regex fails to compile, or is missing the `namespace` or `role` group,
the server **does not fail to start**. It logs one warning and falls back to
`<namespace>:<role>` parsing. With a `_` or `-` format, every permission then
silently stops matching, and every call is denied.

Test your exact regex and some real token values before you deploy. This demo is
a quick way to do that: edit the `PermissionsRegex` and permission lists in
`main.go` (scenarios C and D), then run `go run .`.

## Version requirements

| Component | Requirement |
|---|---|
| Temporal server (self-hosted) | **v1.29.0 or newer**. `permissionsRegex` was added in [temporalio/temporal#7574](https://github.com/temporalio/temporal/pull/7574). Earlier versions only support `<namespace>:<role>`. |
| Demo build | Go 1.26.8+; the demo uses server module v1.32.1 |

This applies to the open-source server's default claim mapper. Temporal Cloud
uses its own authentication and authorization and isn't configured this way.

## Source references (server v1.32.1)

- Config field and its documentation: [`common/config/config.go`](https://github.com/temporalio/temporal/blob/v1.32.1/common/config/config.go) (`Authorization.PermissionsRegex`)
- Regex compilation and fallback: [`common/authorization/default_jwt_claim_mapper.go`](https://github.com/temporalio/temporal/blob/v1.32.1/common/authorization/default_jwt_claim_mapper.go) (`NewDefaultJWTClaimMapper`)
- Per-permission parsing: same file, `extractPermissions`

## Files

| File | Purpose |
|---|---|
| `main.go` | The demo: signs test tokens and runs them through the real claim mapper |
| `go.mod`, `go.sum` | Pinned dependencies (`go.temporal.io/server` v1.32.1) |
| `README.md` | This document |

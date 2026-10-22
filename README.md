# claim-mapper-permission-format-demo

**Temporal SSO with role names that don't use ":"**

## The problem

Temporal's default claim mapper expects roles in the format
`<namespace>:<role>`, for example `payments:write`. Many identity providers
can't emit `:`, so roles look like `payments_write` or
`xyz-deployment_ROLE_write` instead. Two things then go wrong:

1. **The roles don't map.** The server ignores them, so every call is denied.
2. **The Temporal UI won't load,** even once the roles map. After logging in,
   the user is sent straight back to the login page, because
   `GET /api/v1/namespaces` returns **403**.

## The fix

**1. Tell the server how to read your role format** with `permissionsRegex`.
This is a built-in setting; no custom build is needed (server v1.29.0+):

```yaml
global:
  authorization:
    authorizer: default
    claimMapper: default
    permissionsClaimName: roles
    permissionsRegex: '^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$'
```

Get two details exactly right, because both fail silently:

- The key is **`permissionsRegex`**. `PermissionsRegex` and `permissionRegex`
  are ignored, with no error.
- The regex needs **two named groups, `namespace` and `role`**. A regex like
  `_ROLE_` is thrown away, with only a warning in the server log.

**2. Give UI users a system-level role**, for example
`temporal-system_ROLE_read`, in their **access token**. The UI's first two
calls (`ListNamespaces`, `GetClusterInfo`) are cluster-level, and the server
only allows them for users with a `temporal-system` role. Namespace roles,
even `admin`, aren't enough.

## The catch

`temporal-system_ROLE_read` lets the user **read every namespace**, not just
their own. It doesn't let them write anywhere they have no role. If that's not
acceptable, the alternative is a custom server build. See
[Caveats](docs/reference.md#caveats).

## See it in 5 minutes

You need Docker. Stop any `temporal server start-dev` first: it uses the same
ports, and the UI shows a blank page if both are running.

1. **Start** Keycloak, the Temporal server and the UI:

   ```bash
   make up
   ```

   It waits until the stack is ready (about a minute the first time) and
   prints the UI address.

2. **Without the system role:** open http://localhost:8233 in a private
   window, click **Continue to SSO**, and log in as **`ns-only`** with password
   **`demo-password`**.
   You're sent back to the "Welcome back" page. This is the 403 problem.

3. **With the system role:** open a **new** private window, go to
   http://localhost:8233, and log in as **`ns-plus-system`** with the same
   password.
   The Workflows page loads. The fix works.

4. **Stop** everything:

   ```bash
   make down
   ```

Run `make` on its own to list the other shortcuts (`verify`, `up-broken`,
`logs`, `demo`, `test`, and more). Each one prints the `docker compose` or
`go` command it runs, so you can also run that directly.

## More detail

| Document | What's in it |
|---|---|
| [UI walkthrough](docs/ui-walkthrough.md) | All four UI tests, including the caveat and the broken regex, and `verify.sh` |
| [Inspecting tokens in developer tools](docs/ui-walkthrough.md#inspecting-tokens-and-requests-in-developer-tools) | How to see the 403s, find the tokens in the browser, decode them, and check which header carries which token |
| [Go demo](docs/go-demo.md) | `go run .`: runs the real server code on test tokens for every format and failure mode, with no Docker needed |
| [Reference](docs/reference.md) | Troubleshooting, caveats, what was confirmed and how, versions, source links, files |

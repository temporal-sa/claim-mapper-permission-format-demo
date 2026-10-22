# UI walkthrough (`e2e/`)

[← Back to README](../README.md)

This runs a real Temporal server, the Temporal UI and a login server
(Keycloak) on your machine in Docker. You log in as two test users and see the
problem and the fix in the browser. Everything uses stock images, configured
only through environment variables; there is no custom server build.

## Step 1: Before you start

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

## Step 2: Start the stack

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

## Step 3: The test users

Log in to the UI with these. Both use the password **`demo-password`**.

| Username | Roles in the token | What should happen |
|---|---|---|
| `ns-only` | `xyz-deployment_ROLE_read`, `_write`, `_admin` | ❌ UI won't load: you're sent back to the login page |
| `ns-plus-system` | the same, plus `temporal-system_ROLE_read` | ✅ UI loads |

The users are defined in
[`e2e/keycloak/temporal-realm.json`](../e2e/keycloak/temporal-realm.json).

## Step 4: Run the tests in the UI

Open the UI at **http://localhost:8233**. Each test starts in a **new private
window**, so you're logged out.

### Test A: namespace roles only, the UI is blocked (the customer's problem)

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

### Test B: add the system role, the UI works (the fix)

1. Open a new private window and go to http://localhost:8233.
2. Click **Continue to SSO** and log in as **`ns-plus-system`**.

**Expected:** the **Workflows** page for `xyz-deployment` loads (it shows
0 workflows).

### Test C: what the system role also gives access to (the caveat)

1. Still logged in as `ns-plus-system`, click the namespace picker at the top
   left of the page (it shows `xyz-deployment`) and choose **`other-team`**.

**Expected:** you can see the `other-team-sample` workflow, even though this
user has no role on `other-team`. `temporal-system_ROLE_read` gives read
access to **every** namespace. (It doesn't allow writes there; Step 5 shows
that.)

### Test D: the broken regex, nobody gets in (the customer's original config)

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

## Step 5 (optional): the same checks from the command line

[`e2e/verify.sh`](../e2e/verify.sh) logs in as both users and calls the Temporal
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

## Step 6: Stop the stack

To stop and remove everything (the namespaces and workflow are recreated the
next time you start it):

```bash
docker compose -f e2e/compose.yaml down -v
```

To stop it but keep the namespaces and workflow, leave off `-v`:

```bash
docker compose -f e2e/compose.yaml down
```

## Reference: what's running

| Service | Image | Address |
|---|---|---|
| Temporal UI | `temporalio/ui:2.53.3` | http://localhost:8233 |
| Keycloak (login server, realm `temporal`) | `quay.io/keycloak/keycloak:26.4` | http://localhost:8180 (admin console login: `admin` / `admin`) |
| Temporal server | `temporalio/server:1.32.1` | gRPC `localhost:7233`, HTTP API http://localhost:7243 |
| One-shot setup | `temporalio/admin-tools:1.32.1` | creates the namespaces and the sample workflow, then exits |

Keycloak puts the user's roles in a `roles` claim **in the access token only**
(not the ID token), which is what the Temporal server reads.

## Check which token reaches the server

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

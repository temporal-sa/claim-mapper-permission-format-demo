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
make up
```

This runs `docker compose -f e2e/compose.yaml up -d`, then waits for the
one-shot `temporal-setup` container to finish, which takes about a minute
(longer the first time, while images download). It prints `Ready.` and the UI
address when the stack is usable. To check the containers yourself, run
`make ps`: `temporal-setup` should be `Exited (0)` and the other three `Up`.

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
2. Optional: to see the 403, open developer tools on the **Network** tab and
   tick **Keep log** first. See
   [Seeing the 403s in the Network tab](#seeing-the-403s-in-the-network-tab).
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
   make up-broken
   ```

   This runs `PERMISSIONS_REGEX=_ROLE_ docker compose -f e2e/compose.yaml up -d`.

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
   make up
   ```

## Step 5 (optional): the same checks from the command line

[`e2e/verify.sh`](../e2e/verify.sh) logs in as both users and calls the Temporal
API directly, through the same permission checks the UI goes through. It needs
`curl` and `jq`.

```bash
make verify
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
make down
```

To stop it but keep the namespaces and workflow:

```bash
make stop
```

(`make down` runs `docker compose -f e2e/compose.yaml down -v`; `make stop`
leaves off `-v`.)

## Reference: what's running

| Service | Image | Address |
|---|---|---|
| Temporal UI | `temporalio/ui:2.53.3` | http://localhost:8233 |
| Keycloak (login server, realm `temporal`) | `quay.io/keycloak/keycloak:26.4` | http://localhost:8180 (admin console login: `admin` / `admin`) |
| Temporal server | `temporalio/server:1.32.1` | gRPC `localhost:7233`, HTTP API http://localhost:7243 |
| One-shot setup | `temporalio/admin-tools:1.32.1` | creates the namespaces and the sample workflow, then exits |

Keycloak puts the user's roles in a `roles` claim **in the access token only**
(not the ID token), which is what the Temporal server reads.

## Inspecting tokens and requests in developer tools

Use these when you need to see what the browser is actually sending: the 403s,
the tokens, and the roles inside them. All of it stays on your machine.

### Seeing the 403s in the Network tab

1. Open developer tools and go to the **Network** tab.
2. Tick **Keep log** (some versions of Chrome call it **Preserve log**).
3. Optional: click the **Fetch/XHR** filter to hide JavaScript, CSS and images.
4. Log in.

**Keep log** matters because, after the 403, the UI loads a whole new login
page. Chrome clears the Network tab on every page load, so without it the
403s disappear before you can see them.

With the 403 problem you'll see `namespaces` and `cluster-info` with status
**403**, followed by a new `login` request.

### Finding the tokens

The UI keeps both tokens in the browser's local storage:

1. Log in as **`ns-plus-system`**, so you stay logged in.
2. Open developer tools and go to the **Application** tab.
3. In the sidebar, open **Storage → Local storage → http://localhost:8233**.
4. Click the **`AuthUser`** entry. Its value has these fields:

| Field | What it is |
|---|---|
| `accessToken` | The token the Temporal server checks. It must contain `roles`. |
| `idToken` | Identifies who logged in. In this demo it has no `roles`. |
| `email`, `name` | The logged-in user |

The tokens start with `eyJ` because they're encoded. Decode them to read them.

### Decoding the tokens in the browser (easiest)

Paste this into the **Console** tab. It decodes both tokens in your browser,
without sending them anywhere:

```js
const u = JSON.parse(localStorage.getItem('AuthUser'));
const dec = t => JSON.parse(atob(t.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
console.log('access token:', dec(u.accessToken));
console.log('id token:', dec(u.idToken));
```

**Expected for `ns-plus-system`:** the access token has
`roles: ["xyz-deployment_ROLE_write", "temporal-system_ROLE_read", "xyz-deployment_ROLE_read", "xyz-deployment_ROLE_admin"]`
(in any order), and the ID token has no `roles`. Because the UI works even
though the ID token has no roles, the server must be reading the access token.

### Decoding a token in the terminal (macOS)

1. Copy **only the token**: the text starting with `eyJ`, without `Bearer `
   in front and without quotes.
2. Run this. Don't type or paste anything after it; `pbpaste` reads the
   clipboard:

   ```bash
   pbpaste | cut -d. -f2 | tr '_-' '/+' | { read -r p; while (( ${#p} % 4 )); do p+="="; done; echo "$p"; } | base64 -d | jq '{sub, roles}'
   ```

The middle step adds the `=` padding that tokens leave off. Without it, macOS
`base64` drops the end of the data and `jq` fails with
`Unfinished JSON term at EOF`.

Don't paste tokens into public decoding websites. Even in a demo, they're
live credentials until they expire.

### Which header carries which token

The UI sends the **access token** in the `Authorization: Bearer …` header and
the **ID token** in an `Authorization-Extras` header. The Temporal server's
default claim mapper reads only `Authorization`, so the `roles` claim must be
in the **access token**.

To see the headers: with **Keep log** on, click a request such as `namespaces`
and look under **Request Headers**. (The `Authorization-Extras` part comes
from the UI server's log, `Validating ID token from Authorization-Extras
header`; these Network-tab steps haven't been checked yet.)

If `roles` is missing from the access token but present in the ID token, fix
the IdP's claim mapping, for example the Keycloak "realm roles" mapper's
**Add to access token** setting. Don't switch the UI to `useIdTokenAsBearer`
just to get roles through.

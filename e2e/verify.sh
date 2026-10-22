#!/usr/bin/env bash
# Calls the running stack as each demo user and prints what the server allowed.
#
# For each user it gets an access token from Keycloak (password grant), decodes
# it locally, and calls the Temporal frontend HTTP API (localhost:7243) with the
# token in the Authorization header. That goes through the same claim mapper
# and authorizer as the UI's gRPC calls.
#
# Requires: curl, jq, docker compose. Run from anywhere: ./e2e/verify.sh
set -euo pipefail
cd "$(dirname "$0")"

KEYCLOAK=http://localhost:8180/realms/temporal
TEMPORAL_HTTP=http://localhost:7243

# Decode one base64url JWT segment locally (never paste tokens into a website).
jwt_part() {
  local s
  s=$(cut -d. -f"$2" <<<"$1" | tr '_-' '/+')
  while (( ${#s} % 4 )); do s+="="; done
  base64 -d <<<"$s" 2>/dev/null
}

call() { # token method path -> HTTP status
  curl -s -o /dev/null -w '%{http_code}' -X "$2" -H "Authorization: Bearer $1" "$TEMPORAL_HTTP$3" ${4:+-d "$4"}
}

verdict() { case "$1" in 200) echo "ALLOW";; 403) echo "DENY ";; *) echo "HTTP $1";; esac; }

echo "=== Server config ==="
regex=$(docker compose exec -T temporal printenv TEMPORAL_JWT_PERMISSIONS_REGEX || true)
echo "permissionsRegex (env TEMPORAL_JWT_PERMISSIONS_REGEX): ${regex}"
warn=$(docker compose logs temporal 2>/dev/null | grep -o 'permissions regex[^"]*' | sort -u || true)
echo "regex warnings in server log: ${warn:-none}"

for user in ns-only ns-plus-system; do
  token=$(curl -sf -d grant_type=password -d client_id=temporal-cli -d "username=$user" -d password=demo-password \
    "$KEYCLOAK/protocol/openid-connect/token" | jq -r .access_token)

  echo
  echo "=== User: $user ==="
  echo "access token header : $(jwt_part "$token" 1 | jq -c '{alg, kid}')"
  echo "access token claims : $(jwt_part "$token" 2 | jq -c '{sub, roles}')"
  printf '  %s %-28s %s\n' \
    "$(verdict "$(call "$token" GET /api/v1/namespaces)")" ListNamespaces "(cluster; UI calls this on every page)" \
    "$(verdict "$(call "$token" GET /api/v1/cluster-info)")" GetClusterInfo "(cluster; UI calls this on every page)" \
    "$(verdict "$(call "$token" GET /api/v1/namespaces/xyz-deployment/workflows)")" ListWorkflowExecutions "(namespace xyz-deployment: user has a role)" \
    "$(verdict "$(call "$token" GET /api/v1/namespaces/other-team/workflows)")" ListWorkflowExecutions "(namespace other-team: no role in token)" \
    "$(verdict "$(call "$token" GET /api/v1/namespaces/other-team/workflows/other-team-sample/history)")" GetWorkflowExecutionHistory "(namespace other-team: no role in token)" \
    "$(verdict "$(call "$token" POST /api/v1/namespaces/other-team/workflows/verify-$RANDOM \
      '{"workflowType":{"name":"SampleWorkflow"},"taskQueue":{"name":"other-team-tq"}}')")" StartWorkflowExecution "(namespace other-team: no role in token)"
done

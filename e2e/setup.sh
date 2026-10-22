#!/bin/sh
# Runs inside temporalio/admin-tools. Gets an admin token from Keycloak with the
# temporal-admin service account (role temporal-system_ROLE_admin), then creates
# the demo namespaces and one workflow in other-team. Safe to re-run.
set -eu

ADDR=temporal:7233
TOKEN_URL=http://keycloak:8080/realms/temporal/protocol/openid-connect/token

token() {
  wget -q -O - --post-data 'grant_type=client_credentials&client_id=temporal-admin&client_secret=temporal-admin-demo-secret' "$TOKEN_URL" |
    sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p'
}

i=0
until temporal operator cluster health --address "$ADDR" >/dev/null 2>&1; do
  i=$((i + 1)); [ "$i" -gt 60 ] && { echo "temporal not healthy"; exit 1; }
  sleep 2
done

T=$(token)
[ -n "$T" ] || { echo "could not get admin token from Keycloak"; exit 1; }
AUTH="authorization=Bearer $T"

# With a broken permissionsRegex even the admin token maps to nothing. Namespaces
# from an earlier run are kept in the temporal-data volume, so just skip.
if ! temporal operator namespace list --address "$ADDR" --grpc-meta "$AUTH" >/dev/null 2>&1; then
  echo "admin token (temporal-system_ROLE_admin) was denied: permissionsRegex is not mapping roles. Skipping setup."
  exit 0
fi

for ns in xyz-deployment other-team; do
  if temporal operator namespace describe --address "$ADDR" --grpc-meta "$AUTH" -n "$ns" >/dev/null 2>&1; then
    echo "namespace $ns exists"
  else
    temporal operator namespace create --address "$ADDR" --grpc-meta "$AUTH" -n "$ns" --retention 1d
  fi
done

# Namespaces are cached briefly by the frontend; retry until other-team is usable.
i=0
until temporal workflow start --address "$ADDR" --grpc-meta "$AUTH" -n other-team \
  --workflow-id other-team-sample --type SampleWorkflow --task-queue other-team-tq >/dev/null 2>&1 ||
  temporal workflow describe --address "$ADDR" --grpc-meta "$AUTH" -n other-team -w other-team-sample >/dev/null 2>&1; do
  i=$((i + 1)); [ "$i" -gt 30 ] && { echo "could not start sample workflow"; exit 1; }
  sleep 2
done
echo "workflow other-team/other-team-sample present"
echo "setup done"

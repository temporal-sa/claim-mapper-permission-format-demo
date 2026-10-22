package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"go.temporal.io/server/common/api"
	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/log/tag"
)

const roleRegex = `^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$`

var (
	nsOnly       = []string{"xyz-deployment_ROLE_read", "xyz-deployment_ROLE_write", "xyz-deployment_ROLE_admin"}
	nsPlusSystem = append(append([]string{}, nsOnly...), "temporal-system_ROLE_read")
)

// captureLogger records the server's Warn messages.
type captureLogger struct {
	log.Logger
	warnings []string
}

func (l *captureLogger) Warn(msg string, _ ...tag.Tag) { l.warnings = append(l.warnings, msg) }

// claimsFor signs a token with the given roles and maps it with the real claim mapper.
func claimsFor(t *testing.T, cfg *config.Authorization, roles []string) (*authorization.Claims, []string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	logger := &captureLogger{Logger: log.NewNoopLogger()}
	mapper := authorization.NewDefaultJWTClaimMapper(staticKeyProvider{&priv.PublicKey}, cfg, logger)
	claims, err := mapper.GetClaims(&authorization.AuthInfo{AuthToken: "Bearer " + mustToken(priv, cfg.PermissionsClaimName, roles)})
	if err != nil {
		t.Fatalf("GetClaims: %v", err)
	}
	return claims, logger.warnings
}

// allowed asks the real default authorizer whether claims may call method in namespace.
func allowed(t *testing.T, claims *authorization.Claims, method, namespace string) bool {
	t.Helper()
	res, err := authorization.NewDefaultAuthorizer().Authorize(context.Background(), claims, &authorization.CallTarget{
		APIName:   api.WorkflowServicePrefix + method,
		Namespace: namespace,
	})
	if err != nil {
		t.Fatalf("Authorize(%s, %q): %v", method, namespace, err)
	}
	return res.Decision == authorization.DecisionAllow
}

type wantCall struct {
	method, namespace string
	allow             bool
}

func checkCalls(t *testing.T, claims *authorization.Claims, want []wantCall) {
	t.Helper()
	for _, c := range want {
		if got := allowed(t, claims, c.method, c.namespace); got != c.allow {
			t.Errorf("%s (namespace %q): allowed=%v, want %v", c.method, c.namespace, got, c.allow)
		}
	}
}

// Scenario E: every role maps, but the UI's cluster-scoped calls are denied.
func TestNamespaceRolesOnlyDenyUIClusterCalls(t *testing.T) {
	claims, _ := claimsFor(t, &config.Authorization{PermissionsClaimName: "roles", PermissionsRegex: roleRegex}, nsOnly)

	if claims.System != authorization.RoleUndefined {
		t.Errorf("system role = %v, want none", claims.System)
	}
	want := authorization.RoleReader | authorization.RoleWriter | authorization.RoleAdmin
	if got := claims.Namespaces["xyz-deployment"]; got != want {
		t.Errorf("xyz-deployment role = %v, want %v", got, want)
	}
	checkCalls(t, claims, []wantCall{
		{"ListNamespaces", "", false},
		{"GetClusterInfo", "", false},
		{"ListWorkflowExecutions", "xyz-deployment", true},
		{"ListWorkflowExecutions", "other-team", false},
	})
}

// Scenario F: temporal-system_ROLE_read allows the UI's calls and read (not
// write) access to namespaces the token says nothing about.
func TestSystemReadAllowsUIAndReadsEveryNamespace(t *testing.T) {
	claims, _ := claimsFor(t, &config.Authorization{PermissionsClaimName: "roles", PermissionsRegex: roleRegex}, nsPlusSystem)

	if claims.System != authorization.RoleReader {
		t.Errorf("system role = %v, want reader", claims.System)
	}
	checkCalls(t, claims, []wantCall{
		{"ListNamespaces", "", true},
		{"GetClusterInfo", "", true},
		{"ListWorkflowExecutions", "xyz-deployment", true},
		{"ListWorkflowExecutions", "other-team", true},
		{"GetWorkflowExecutionHistory", "other-team", true},
		{"StartWorkflowExecution", "other-team", false},
	})
}

// Scenario G: a regex without named groups is discarded with a warning and
// nothing maps.
func TestRegexWithoutNamedGroupsFallsBackSilently(t *testing.T) {
	claims, warnings := claimsFor(t, &config.Authorization{PermissionsClaimName: "roles", PermissionsRegex: `_ROLE_`}, nsPlusSystem)

	if claims.System != authorization.RoleUndefined || len(claims.Namespaces) != 0 {
		t.Errorf("got system=%v namespaces=%v, want nothing mapped", claims.System, claims.Namespaces)
	}
	const warning = "permissions regex does not have namespace or role named group"
	if len(warnings) == 0 || warnings[0] != warning {
		t.Errorf("first warning = %q, want %q", warnings, warning)
	}
	checkCalls(t, claims, []wantCall{
		{"ListNamespaces", "", false},
		{"ListWorkflowExecutions", "xyz-deployment", false},
	})
}

// Scenario H: only the exact YAML key "permissionsRegex" is loaded.
func TestOnlyExactYAMLKeyIsLoaded(t *testing.T) {
	for key, want := range map[string]string{
		"permissionsRegex": roleRegex,
		"PermissionsRegex": "",
		"permissionRegex":  "",
	} {
		t.Run(key, func(t *testing.T) {
			cfg, err := loadAuthConfig(key)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if cfg.PermissionsRegex != want {
				t.Errorf("loaded permissionsRegex = %q, want %q", cfg.PermissionsRegex, want)
			}
		})
	}
}

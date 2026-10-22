// Demo: Temporal's default JWT ClaimMapper and Authorizer with and without permissionsRegex.
//
// It signs test JWTs with a throwaway RSA key and feeds them to the REAL
// authorization.NewDefaultJWTClaimMapper from go.temporal.io/server, using the
// same config struct the server reads from YAML (global.authorization). The
// "_ROLE_" scenarios also run the resulting claims through the REAL
// authorization.NewDefaultAuthorizer for the calls the Temporal UI makes.
//
// Scenarios:
//   A. Default config + "<namespace>:<role>"   -> works (baseline)
//   B. Default config + "<namespace>_<role>"   -> nothing maps (the customer's problem)
//   C. permissionsRegex for "_" separator      -> works
//   D. permissionsRegex for "-" separator      -> works
//   E. "_ROLE_" regex, namespace roles only    -> maps, but UI calls are denied
//   F. "_ROLE_" regex + temporal-system read   -> UI calls allowed (reaches every namespace)
//   G. permissionsRegex "_ROLE_" (no groups)   -> regex discarded, nothing maps
//   H. YAML key spelling, via the real config loader
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"go.temporal.io/server/common/api"
	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/log"
	"go.temporal.io/server/common/log/tag"
)

// staticKeyProvider hands the mapper a single RSA public key (stands in for JWKS).
type staticKeyProvider struct{ pub *rsa.PublicKey }

func (p staticKeyProvider) EcdsaKey(alg, kid string) (*ecdsa.PublicKey, error) {
	return nil, errors.New("ecdsa not supported in demo")
}
func (p staticKeyProvider) HmacKey(alg, kid string) ([]byte, error) {
	return nil, errors.New("hmac not supported in demo")
}
func (p staticKeyProvider) RsaKey(alg, kid string) (*rsa.PublicKey, error) { return p.pub, nil }
func (p staticKeyProvider) SupportedMethods() []string                     { return []string{"RS256"} }
func (p staticKeyProvider) Close()                                         {}

// warnLogger prints the server's Warn messages so silent fallbacks are visible.
type warnLogger struct{ log.Logger }

func (warnLogger) Warn(msg string, _ ...tag.Tag) { fmt.Printf("server log WARN : %s\n", msg) }

func mustToken(priv *rsa.PrivateKey, claimName string, permissions []string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":     "demo-service-account",
		claimName: permissions,
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	t.Header["kid"] = "demo-key" // the default mapper requires a kid header
	s, err := t.SignedString(priv)
	if err != nil {
		panic(err)
	}
	return s
}

func roleName(r authorization.Role) string {
	var out []string
	if r&authorization.RoleAdmin != 0 {
		out = append(out, "admin")
	}
	if r&authorization.RoleWriter != 0 {
		out = append(out, "write")
	}
	if r&authorization.RoleReader != 0 {
		out = append(out, "read")
	}
	if r&authorization.RoleWorker != 0 {
		out = append(out, "worker")
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, "+")
}

// uiCalls are the cluster-scoped calls the UI makes on every page load, plus
// namespace-scoped calls in the user's own namespace and in a namespace the
// token says nothing about.
var uiCalls = []struct{ method, namespace string }{
	{"ListNamespaces", ""},
	{"GetClusterInfo", ""},
	{"ListWorkflowExecutions", "xyz-deployment"},
	{"ListWorkflowExecutions", "other-team"},
	{"GetWorkflowExecutionHistory", "other-team"},
	{"StartWorkflowExecution", "other-team"},
}

func authorize(claims *authorization.Claims) {
	authorizer := authorization.NewDefaultAuthorizer()
	fmt.Println("AUTHORIZER:")
	for _, c := range uiCalls {
		res, err := authorizer.Authorize(context.Background(), claims, &authorization.CallTarget{
			APIName:   api.WorkflowServicePrefix + c.method,
			Namespace: c.namespace,
		})
		decision := "DENY "
		if err == nil && res.Decision == authorization.DecisionAllow {
			decision = "ALLOW"
		}
		scope := "cluster"
		if c.namespace != "" {
			scope = "namespace " + c.namespace
		}
		fmt.Printf("        %s %-28s (%s)\n", decision, c.method, scope)
	}
}

func run(label string, pub *rsa.PublicKey, cfg *config.Authorization, token string, perms []string, checkAuthz bool) {
	fmt.Printf("\n=== %s ===\n", label)
	fmt.Printf("permissionsRegex : %q\n", cfg.PermissionsRegex)
	fmt.Printf("token %-11s: %v\n", cfg.PermissionsClaimName, perms)
	mapper := authorization.NewDefaultJWTClaimMapper(staticKeyProvider{pub}, cfg, warnLogger{log.NewNoopLogger()})
	claims, err := mapper.GetClaims(&authorization.AuthInfo{AuthToken: "Bearer " + token})
	if err != nil {
		fmt.Printf("RESULT: error: %v\n", err)
		return
	}
	fmt.Printf("RESULT: subject=%s system=%s\n", claims.Subject, roleName(claims.System))
	names := make([]string, 0, len(claims.Namespaces))
	for ns := range claims.Namespaces {
		names = append(names, ns)
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Println("        namespaces: (none mapped)")
	}
	for _, ns := range names {
		fmt.Printf("        namespace %-18s -> %s\n", ns, roleName(claims.Namespaces[ns]))
	}
	if checkAuthz {
		authorize(claims)
	}
}

// configYAML is a minimal server config. Only the regex key under
// global.authorization changes between the variants in scenario H.
const configYAML = `persistence:
  numHistoryShards: 1
  defaultStore: default
  visibilityStore: visibility
  datastores:
    default:
      sql: {pluginName: sqlite, databaseName: temporal, connectAddr: localhost, connectProtocol: tcp, connectAttributes: {mode: memory}}
    visibility:
      sql: {pluginName: sqlite, databaseName: temporal_visibility, connectAddr: localhost, connectProtocol: tcp, connectAttributes: {mode: memory}}
global:
  authorization:
    authorizer: default
    claimMapper: default
    permissionsClaimName: roles
    %s: '^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$'
`

// loadAuthConfig writes configYAML with the given regex key and loads it with
// the server's own config loader.
func loadAuthConfig(key string) (*config.Authorization, error) {
	path := filepath.Join(os.TempDir(), "permissionsregex-demo-"+key+".yaml")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(configYAML, key)), 0o600); err != nil {
		return nil, err
	}
	defer os.Remove(path)
	cfg, err := config.Load(config.WithConfigFile(path))
	if err != nil {
		return nil, err
	}
	return &cfg.Global.Authorization, nil
}

func main() {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	pub := &priv.PublicKey

	colonPerms := []string{"temporal-system:read", "payments:write", "orders:read"}
	underscorePerms := []string{"temporal-system_read", "payments_write", "payments_prod_admin", "orders_read", "billing_viewer"}
	hyphenPerms := []string{"temporal-system-read", "payments-write", "payments-prod-admin", "orders-read", "billing-viewer"}
	nsOnlyRoles := []string{"xyz-deployment_ROLE_read", "xyz-deployment_ROLE_write", "xyz-deployment_ROLE_admin"}
	nsPlusSystemRoles := append(append([]string{}, nsOnlyRoles...), "temporal-system_ROLE_read")

	// A. Baseline: default behavior, "<namespace>:<role>".
	cfgDefault := &config.Authorization{PermissionsClaimName: "permissions"}
	run("A. Default config, colon format (baseline)", pub, cfgDefault, mustToken(priv, "permissions", colonPerms), colonPerms, false)

	// B. Same default config, but the IdP can only emit "_" -> nothing maps.
	run("B. Default config, underscore format (the problem)", pub, cfgDefault, mustToken(priv, "permissions", underscorePerms), underscorePerms, false)

	// C. permissionsRegex for "_". Namespace is greedy; the role is anchored to the
	//    four valid roles so namespaces may themselves contain "_".
	cfgUnderscore := &config.Authorization{
		PermissionsClaimName: "permissions",
		PermissionsRegex:     `^(?P<namespace>.+)_(?P<role>read|write|worker|admin)$`,
	}
	run("C. permissionsRegex, underscore separator", pub, cfgUnderscore, mustToken(priv, "permissions", underscorePerms), underscorePerms, false)

	// D. permissionsRegex for "-".
	cfgHyphen := &config.Authorization{
		PermissionsClaimName: "permissions",
		PermissionsRegex:     `^(?P<namespace>.+)-(?P<role>read|write|worker|admin)$`,
	}
	run("D. permissionsRegex, hyphen separator", pub, cfgHyphen, mustToken(priv, "permissions", hyphenPerms), hyphenPerms, false)

	// E. "_ROLE_" separator in a "roles" claim, namespace roles only. Every role
	//    maps, but the UI's cluster-scoped calls only look at the system role.
	cfgRole := &config.Authorization{
		PermissionsClaimName: "roles",
		PermissionsRegex:     `^(?P<namespace>.+)_ROLE_(?P<role>read|write|worker|admin)$`,
	}
	run("E. _ROLE_ regex, namespace roles only (UI gets 403)", pub, cfgRole, mustToken(priv, "roles", nsOnlyRoles), nsOnlyRoles, true)

	// F. Same, plus temporal-system_ROLE_read.
	run("F. _ROLE_ regex, plus temporal-system_ROLE_read (UI loads)", pub, cfgRole, mustToken(priv, "roles", nsPlusSystemRoles), nsPlusSystemRoles, true)

	// G. A regex without the named groups is discarded with a warning and the
	//    mapper falls back to "<namespace>:<role>".
	cfgNoGroups := &config.Authorization{PermissionsClaimName: "roles", PermissionsRegex: `_ROLE_`}
	run("G. permissionsRegex without named groups (silent fallback)", pub, cfgNoGroups, mustToken(priv, "roles", nsPlusSystemRoles), nsPlusSystemRoles, true)

	// H. The YAML key must be spelled exactly "permissionsRegex". Load the same
	//    config with three spellings through the server's config loader.
	fmt.Println("\n=== H. YAML key spelling, loaded by the server's config loader ===")
	for _, key := range []string{"permissionsRegex", "PermissionsRegex", "permissionRegex"} {
		cfg, err := loadAuthConfig(key)
		if err != nil {
			fmt.Printf("%-17s: load error: %v\n", key, err)
			continue
		}
		fmt.Printf("%-17s: loaded permissionsRegex = %q\n", key, cfg.PermissionsRegex)
	}
}

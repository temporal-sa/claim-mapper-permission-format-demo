// Demo: Temporal's default JWT ClaimMapper with and without permissionsRegex.
//
// It signs test JWTs with a throwaway RSA key and feeds them to the REAL
// authorization.NewDefaultJWTClaimMapper from go.temporal.io/server, using the
// same config struct the server reads from YAML (global.authorization).
//
// Scenarios:
//   A. Default config + "<namespace>:<role>"   -> works (baseline)
//   B. Default config + "<namespace>_<role>"   -> nothing maps (the customer's problem)
//   C. permissionsRegex for "_" separator      -> works
//   D. permissionsRegex for "-" separator      -> works
package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/config"
	"go.temporal.io/server/common/log"
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

func mustToken(priv *rsa.PrivateKey, permissions []string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":         "demo-service-account",
		"permissions": permissions,
		"exp":         time.Now().Add(time.Hour).Unix(),
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

func run(label string, pub *rsa.PublicKey, cfg *config.Authorization, token string, perms []string) {
	mapper := authorization.NewDefaultJWTClaimMapper(staticKeyProvider{pub}, cfg, log.NewNoopLogger())
	claims, err := mapper.GetClaims(&authorization.AuthInfo{AuthToken: "Bearer " + token})

	fmt.Printf("\n=== %s ===\n", label)
	fmt.Printf("permissionsRegex : %q\n", cfg.PermissionsRegex)
	fmt.Printf("token permissions: %v\n", perms)
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

	// A. Baseline: default behavior, "<namespace>:<role>".
	cfgDefault := &config.Authorization{PermissionsClaimName: "permissions"}
	run("A. Default config, colon format (baseline)", pub, cfgDefault, mustToken(priv, colonPerms), colonPerms)

	// B. Same default config, but the IdP can only emit "_" -> nothing maps.
	run("B. Default config, underscore format (the problem)", pub, cfgDefault, mustToken(priv, underscorePerms), underscorePerms)

	// C. permissionsRegex for "_". Namespace is greedy; the role is anchored to the
	//    four valid roles so namespaces may themselves contain "_".
	cfgUnderscore := &config.Authorization{
		PermissionsClaimName: "permissions",
		PermissionsRegex:     `^(?P<namespace>.+)_(?P<role>read|write|worker|admin)$`,
	}
	run("C. permissionsRegex, underscore separator", pub, cfgUnderscore, mustToken(priv, underscorePerms), underscorePerms)

	// D. permissionsRegex for "-".
	cfgHyphen := &config.Authorization{
		PermissionsClaimName: "permissions",
		PermissionsRegex:     `^(?P<namespace>.+)-(?P<role>read|write|worker|admin)$`,
	}
	run("D. permissionsRegex, hyphen separator", pub, cfgHyphen, mustToken(priv, hyphenPerms), hyphenPerms)
}

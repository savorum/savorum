package account

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/identity"
)

type passkeyListResponse struct {
	Passkeys  []identity.Passkey `json:"passkeys"`
	Available bool               `json:"available" doc:"Whether the current origin can register or use a passkey"`
}
type credentialLoginResponse struct {
	User                  accountUser `json:"user"`
	CSRFToken             string      `json:"csrf_token"`
	RecoveryAuthenticated bool        `json:"recovery_authenticated"`
}
type recoveryStatusResponse struct {
	Remaining int  `json:"remaining" minimum:"0"`
	Allowed   bool `json:"allowed" doc:"Whether provider policy allows recovery codes for this account"`
}
type recoveryCodesResponse struct {
	Codes []string `json:"codes" doc:"Shown once; only their hashes are stored"`
}

// RegisterPasskeyRecoveryOpenAPI documents the browser WebAuthn and recovery boundary.
func RegisterPasskeyRecoveryOpenAPI(api huma.API) {
	for _, route := range []struct {
		method, path, id, tag, summary, detail string
		public                                 bool
	}{
		{"GET", "/api/v1/account/passkeys", "list-passkeys", "Passkeys", "List passkeys", "Includes whether the current origin can register or use one.", false},
		{"POST", "/api/v1/account/passkeys/register/begin", "begin-passkey-registration", "Passkeys", "Begin passkey registration", "Requests a discoverable, user-verified credential. Requires recent authentication.", false},
		{"POST", "/api/v1/account/passkeys/register/finish", "finish-passkey-registration", "Passkeys", "Finish passkey registration", "Verifies the creation response and stores only public material.", false},
		{"PUT", "/api/v1/account/passkeys/{id}", "rename-passkey", "Passkeys", "Rename a passkey", "Requires recent authentication.", false},
		{"DELETE", "/api/v1/account/passkeys/{id}", "revoke-passkey", "Passkeys", "Revoke a passkey", "Refused when it would leave no usable sign-in or recovery method.", false},
		{"POST", "/api/v1/account/passkeys/login/begin", "begin-passkey-login", "Passkeys", "Begin passkey sign-in", "Usernameless: the authenticator supplies the account.", true},
		{"POST", "/api/v1/account/passkeys/login/finish", "finish-passkey-login", "Passkeys", "Finish passkey sign-in", "Verifies the assertion and rotates the browser session.", true},
		{"GET", "/api/v1/account/recovery", "get-recovery-status", "Recovery codes", "Get recovery status", "Reports the unused code count and the provider recovery policy.", false},
		{"POST", "/api/v1/account/recovery", "generate-recovery-codes", "Recovery codes", "Generate recovery codes", "Replaces any existing set and shows ten codes exactly once. Requires recent authentication.", false},
		{"POST", "/api/v1/account/recovery/login", "login-recovery-code", "Recovery codes", "Sign in with a recovery code", "Consumes one code and establishes a recovery-marked session.", true},
	} {
		op := &huma.Operation{Method: route.method, Path: route.path, OperationID: route.id, Summary: route.summary, Tags: []string{route.tag}, Description: route.detail + " Browser session authentication and CSRF apply to protected mutations. Recent authentication means a session created within five minutes. WebAuthn challenges are one-use, browser-bound and expire after five minutes. Public sign-in requires the configured Origin header. Responses must not be cached."}
		if strings.Contains(route.path, "{id}") {
			op.Parameters = append(op.Parameters, &huma.Param{Name: "id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString}})
		}

		if route.public {
			op.Parameters = append(op.Parameters, &huma.Param{Name: "Origin", In: "header", Required: true, Schema: &huma.Schema{Type: huma.TypeString}})
		} else if route.method != http.MethodGet {
			op.Parameters = append(op.Parameters, &huma.Param{Name: "X-CSRF-Token", In: "header", Required: true, Schema: &huma.Schema{Type: huma.TypeString}})
		}

		if route.id == "finish-passkey-registration" {
			op.Parameters = append(op.Parameters, &huma.Param{Name: "name", In: "query", Required: true, Schema: &huma.Schema{Type: huma.TypeString}})
		}

		object := &huma.Schema{Type: huma.TypeObject, AdditionalProperties: true}
		if strings.HasPrefix(route.id, "finish-passkey") || route.id == "rename-passkey" || route.id == "login-recovery-code" {
			schema := object
			if route.id == "rename-passkey" {
				schema = &huma.Schema{Type: huma.TypeObject, Required: []string{"name"}, Properties: map[string]*huma.Schema{"name": {Type: huma.TypeString}}, AdditionalProperties: false}
			}

			if route.id == "login-recovery-code" {
				schema = &huma.Schema{Type: huma.TypeObject, Required: []string{"login", "code"}, Properties: map[string]*huma.Schema{"login": {Type: huma.TypeString}, "code": {Type: huma.TypeString, WriteOnly: true}}, AdditionalProperties: false}
			}

			op.RequestBody = &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: schema}}}
		}

		registry := api.OpenAPI().Components.Schemas
		response := &huma.Schema{Type: huma.TypeObject, Properties: map[string]*huma.Schema{}, AdditionalProperties: false}

		switch route.id {
		case "list-passkeys":
			response = registry.Schema(reflect.TypeFor[passkeyListResponse](), true, "PasskeyListResponse")
		case "begin-passkey-registration", "begin-passkey-login":
			response = &huma.Schema{Type: huma.TypeObject, Required: []string{"publicKey"}, AdditionalProperties: true, Properties: map[string]*huma.Schema{
				"publicKey": {Type: huma.TypeObject, AdditionalProperties: true, Description: "WebAuthn PublicKeyCredentialCreationOptions or PublicKeyCredentialRequestOptions, as the browser API takes them."},
			}}
		case "finish-passkey-login", "login-recovery-code":
			response = registry.Schema(reflect.TypeFor[credentialLoginResponse](), true, "CredentialLoginResponse")
		case "get-recovery-status":
			response = registry.Schema(reflect.TypeFor[recoveryStatusResponse](), true, "RecoveryStatusResponse")
		case "generate-recovery-codes":
			response = registry.Schema(reflect.TypeFor[recoveryCodesResponse](), true, "RecoveryCodesResponse")
		}

		op.Responses = map[string]*huma.Response{
			"200":     {Description: "Completed.", Content: map[string]*huma.MediaType{"application/json": {Schema: response}}},
			"default": {Description: "Authentication, policy, rate, or protocol validation failure.", Content: map[string]*huma.MediaType{"application/problem+json": {Schema: registry.Schema(reflect.TypeFor[accountProblem](), true, "AccountProblem")}}},
		}
		api.OpenAPI().AddOperation(op)
	}
}

package v1

import (
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

const (
	bearerScheme  = "bearerToken"
	sessionScheme = "browserSession"
)

// Operations that answer without credentials. The account middleware and the
// federation handler own these exemptions; this list only describes them.
var (
	anonymousOperations = map[string]bool{
		"get-health":                     true,
		"login-account":                  true,
		"register-invited-local-account": true,
		"begin-passkey-login":            true,
		"finish-passkey-login":           true,
		"login-recovery-code":            true,
		"list-authentication-providers":  true,
		"begin-managed-browser-login":    true,
		"complete-oidc-login":            true,
		"complete-saml-login":            true,
		"get-saml-metadata":              true,
	}
	optionalOperations = map[string][]map[string][]string{
		"get-current-account": {{}, {bearerScheme: {}}, {sessionScheme: {}}},
		"get-capabilities":    {{}, {bearerScheme: {}}, {sessionScheme: {}}},
		// Linking a provider to the signed-in account reads the browser session.
		"begin-provider-login": {{}, {sessionScheme: {}}},
	}
)

// describeSecurity declares the two credentials, which operations accept none,
// and the credential failures the middleware returns before any handler runs.
// It runs after every operation is registered.
func describeSecurity(document *huma.OpenAPI) {
	if document.Components.SecuritySchemes == nil {
		document.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}

	local := " An instance in local mode accepts requests without it."
	document.Components.SecuritySchemes[bearerScheme] = &huma.SecurityScheme{
		Type: "http", Scheme: "bearer",
		Description: "A personal API token." + local,
	}
	document.Components.SecuritySchemes[sessionScheme] = &huma.SecurityScheme{
		Type: "apiKey", In: "cookie", Name: "savorum_session",
		Description: "A browser session; hosted instances use `__Host-savorum_session`, declared by their running OpenAPI document. Writes also send the session's `X-CSRF-Token` header." + local,
	}
	document.Security = []map[string][]string{{bearerScheme: {}}, {sessionScheme: {}}}

	for path, item := range document.Paths {
		for _, operation := range []*huma.Operation{item.Get, item.Put, item.Post, item.Delete, item.Options, item.Head, item.Patch, item.Trace} {
			if operation == nil {
				continue
			}

			if anonymousOperations[operation.OperationID] {
				operation.Security = []map[string][]string{}
			} else if requirements, ok := optionalOperations[operation.OperationID]; ok {
				operation.Security = requirements
			}

			describeCredentialFailure(path, operation)
		}
	}
}

func describeCredentialFailure(path string, operation *huma.Operation) {
	var status, description string

	switch {
	case operation.OperationID == "get-health":
		return
	case strings.HasPrefix(path, "/api/v1/auth/"):
		status, description = "403", "The request carries an `Authorization` header, which provider sign-in does not accept (`token_scope_required`)."
	case operation.Security == nil:
		status, description = "401", "No valid session or API token (`authentication-required`), or the credential sent is invalid (`invalid-credentials`)."
	default:
		status, description = "401", "The credential sent is invalid (`invalid-credentials`)."
	}

	if operation.Responses == nil {
		operation.Responses = map[string]*huma.Response{}
	}

	if _, documented := operation.Responses[status]; documented {
		return
	}

	operation.Responses[status] = &huma.Response{Description: description, Content: map[string]*huma.MediaType{
		"application/problem+json": {Schema: &huma.Schema{Ref: "#/components/schemas/AccountProblem"}},
	}}
}

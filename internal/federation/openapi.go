package federation

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// RegisterOpenAPI describes provider routes handled outside the ordinary account middleware.
func RegisterOpenAPI(api huma.API) {
	str := &huma.Schema{Type: huma.TypeString}
	jsonBody := func(schema *huma.Schema) map[string]*huma.MediaType {
		return map[string]*huma.MediaType{"application/json": {Schema: schema}}
	}
	redirect := &huma.Schema{Type: huma.TypeObject, Required: []string{"authorization_url"}, Properties: map[string]*huma.Schema{
		"authorization_url": {Type: huma.TypeString, Format: "uri", Description: "Where to send the browser to continue with an OIDC, SAML or managed provider."},
	}}
	// The account package registers AccountUser before this runs.
	session := &huma.Schema{Type: huma.TypeObject, Required: []string{"user", "csrf_token", "invitation_accepted"}, Properties: map[string]*huma.Schema{
		"user":                {Ref: "#/components/schemas/AccountUser"},
		"csrf_token":          str,
		"invitation_accepted": {Type: huma.TypeBoolean},
	}}
	problem := map[string]*huma.MediaType{"application/problem+json": {Schema: &huma.Schema{Type: huma.TypeObject, Required: []string{"type", "title", "status", "code", "detail"}, Properties: map[string]*huma.Schema{"type": str, "title": str, "status": {Type: huma.TypeInteger}, "code": str, "detail": str}}}}
	signedIn := &huma.Response{Description: "Successful sign-in creates an ordinary secure session and opens account settings, or the library home for a managed provider. A sign-in or link started at the native entry instead redirects to its return target with `code` and `state`, or with `error` (`access_denied` or `authentication_failed`) and `state`, and sets no browser session. A link code changes nothing until `finish-native-provider-link` confirms it."}

	for _, route := range []struct {
		method, path, id, summary string
		responses                 map[string]*huma.Response
	}{
		{http.MethodGet, "/api/v1/auth/providers", "list-authentication-providers", "List sign-in providers", map[string]*huma.Response{
			"200": {Description: "The configured providers, ordered by ID.", Content: jsonBody(&huma.Schema{Type: huma.TypeObject, Required: []string{"providers"}, Properties: map[string]*huma.Schema{
				"providers": {Type: huma.TypeArray, Items: &huma.Schema{Type: huma.TypeObject, Required: []string{"id", "kind", "display_name"}, Properties: map[string]*huma.Schema{
					"id": str, "kind": {Type: huma.TypeString, Enum: []any{"oidc", "saml", "ldap", "proxy", "managed"}}, "display_name": str,
				}}},
			}})},
		}},
		{http.MethodPost, "/api/v1/auth/{provider}/login", "begin-provider-login", "Begin a provider sign-in", map[string]*huma.Response{
			"200": {Description: "OIDC, SAML and managed providers return the authorization_url to navigate to. LDAP and trusted-proxy providers sign in directly and return the new session.", Content: jsonBody(&huma.Schema{OneOf: []*huma.Schema{redirect, session}})},
		}},
		{http.MethodGet, "/api/v1/auth/{provider}/start", "begin-managed-browser-login", "Open a managed library", map[string]*huma.Response{"303": {Description: "Starts a browser-bound managed sign-in. The hosting service confirms its account before the instance replaces the browser session."}}},
		{http.MethodPost, "/api/v1/auth/{provider}/managed-sessions", "finish-managed-native-login", "Redeem a managed session grant", map[string]*huma.Response{"200": {Description: "An ordinary local session after one-use grant, PKCE and local membership checks.", Content: jsonBody(session)}}},
		{http.MethodGet, "/api/v1/auth/{provider}/native", "begin-native-provider-login", "Begin a native sign-in", map[string]*huma.Response{
			"303": {Description: "Sets the browser-bound transaction cookie and redirects to the OIDC, SAML or managed provider. Open this URL in the app's authentication browser, not with a native HTTP client."},
		}},
		{http.MethodGet, "/api/v1/auth/native-browser", "begin-native-browser-login", "Open browser sign-in", map[string]*huma.Response{"303": {Description: "Opens the sign-in page with a five-minute browser-bound transaction. Accepts the same redirect_uri, state, code_challenge, and S256 code_challenge_method as provider sign-in."}}},
		{http.MethodPost, "/api/v1/auth/native-browser/finish", "complete-native-browser-login", "Return sign-in to the app", map[string]*huma.Response{"200": {Description: "Requires a browser session created after entry, Origin, CSRF, and the transaction cookie. Returns redirect_uri with the app state and a one-minute, one-use code bound to PKCE.", Content: jsonBody(&huma.Schema{Type: huma.TypeObject, Required: []string{"redirect_uri"}, Properties: map[string]*huma.Schema{"redirect_uri": str}})}}},
		{http.MethodPost, "/api/v1/auth/native-sessions", "finish-native-provider-login", "Finish a native sign-in", map[string]*huma.Response{
			"200": {Description: "The one-time code and its PKCE verifier are exchanged for an ordinary session, returned in the instance's session cookie. A presented earlier session is revoked.", Content: jsonBody(session)},
		}},
		{http.MethodPost, "/api/v1/auth/{provider}/native-link-tickets", "create-native-link-ticket", "Start a native link", map[string]*huma.Response{
			"200": {Description: "Requires authentication within the last five minutes. Returns a one-minute, single-use ticket bound to the current session and the PKCE challenge. Pass it as `link_ticket` to `begin-native-provider-login` with the same challenge.", Content: jsonBody(&huma.Schema{Type: huma.TypeObject, Required: []string{"link_ticket"}, Properties: map[string]*huma.Schema{"link_ticket": str}})},
		}},
		{http.MethodPost, "/api/v1/auth/native-links", "finish-native-provider-link", "Finish a native link", map[string]*huma.Response{
			"200": {Description: "Requires authentication within the last five minutes. The provider identity is linked to the current account. Linking ends the account's sessions, so the current session is replaced by a new one, returned in the instance's session cookie.", Content: jsonBody(session)},
			"400": {Description: "`invalid_grant`: the code is unknown, expired, already used, issued to another session, or does not match the verifier. Nothing is linked and the current session remains.", Content: problem},
			"409": {Description: "`identity_conflict`: the provider identity belongs to another account. Nothing is linked and the current session remains.", Content: problem},
		}},
		{http.MethodGet, "/api/v1/auth/{provider}/callback", "complete-oidc-login", "Complete browser sign-in", map[string]*huma.Response{"303": signedIn}},
		{http.MethodPost, "/api/v1/auth/{provider}/acs", "complete-saml-login", "Complete a SAML sign-in", map[string]*huma.Response{"303": signedIn}},
		{http.MethodGet, "/api/v1/auth/{provider}/metadata", "get-saml-metadata", "Get SAML metadata", map[string]*huma.Response{
			"200": {Description: "SP metadata; entity ID and exact ACS URL.", Content: map[string]*huma.MediaType{"application/samlmetadata+xml": {Schema: str}}},
		}},
	} {
		op := &huma.Operation{Method: route.method, Path: route.path, OperationID: route.id, Summary: route.summary, Tags: []string{"Authentication"}, Description: "Available only when account authentication is enabled. Provider configuration is restart-bound. Local logout does not require an external provider. Responses must not be cached.", Responses: route.responses}
		if route.id != "list-authentication-providers" && route.id != "finish-native-provider-login" && route.id != "finish-native-provider-link" && route.id != "begin-native-browser-login" && route.id != "complete-native-browser-login" {
			op.Parameters = []*huma.Param{{Name: "provider", In: "path", Required: true, Schema: str}}
		}

		switch route.id {
		case "finish-managed-native-login":
			op.RequestBody = &huma.RequestBody{Required: true, Content: jsonBody(&huma.Schema{Type: huma.TypeObject, AdditionalProperties: false, Required: []string{"grant", "code_verifier"}, Properties: map[string]*huma.Schema{"grant": {Type: huma.TypeString, WriteOnly: true}, "code_verifier": {Type: huma.TypeString, WriteOnly: true}}})}
		case "begin-provider-login":
			op.Parameters = append(op.Parameters, &huma.Param{Name: "Origin", In: "header", Required: true, Schema: str}, &huma.Param{Name: "X-CSRF-Token", In: "header", Description: "Required when explicitly linking to the current account.", Schema: str})
			op.RequestBody = &huma.RequestBody{Required: true, Content: jsonBody(&huma.Schema{Type: huma.TypeObject, AdditionalProperties: false, Properties: map[string]*huma.Schema{"login": str, "password": {Type: huma.TypeString, WriteOnly: true}, "invitation_token": {Type: huma.TypeString, WriteOnly: true}, "link": {Type: huma.TypeBoolean, Description: "Requires a browser session authenticated within the last five minutes and CSRF proof; callback rechecks session validity and freshness."}}})}
		case "begin-native-browser-login", "begin-native-provider-login":
			op.Parameters = append(op.Parameters,
				&huma.Param{Name: "redirect_uri", In: "query", Required: true, Description: "An exactly permitted return target: `savorum://auth/callback`.", Schema: str},
				&huma.Param{Name: "state", In: "query", Required: true, Description: "16 to 256 unreserved characters, returned unchanged to the app.", Schema: str},
				&huma.Param{Name: "code_challenge", In: "query", Required: true, Description: "Base64url SHA-256 of the app's code verifier, without padding.", Schema: str},
				&huma.Param{Name: "code_challenge_method", In: "query", Required: true, Schema: &huma.Schema{Type: huma.TypeString, Enum: []any{"S256"}}})
			if route.id == "begin-native-provider-login" {
				op.Parameters = append(op.Parameters, &huma.Param{Name: "link_ticket", In: "query", Description: "A ticket from `create-native-link-ticket`. The return then carries a link code for `finish-native-provider-link` instead of a sign-in code.", Schema: str})
			}
		case "create-native-link-ticket":
			op.Parameters = append(op.Parameters, &huma.Param{Name: "Origin", In: "header", Required: true, Schema: str}, &huma.Param{Name: "X-CSRF-Token", In: "header", Required: true, Schema: str})
			op.RequestBody = &huma.RequestBody{Required: true, Content: jsonBody(&huma.Schema{Type: huma.TypeObject, AdditionalProperties: false, Required: []string{"code_challenge", "code_challenge_method"}, Properties: map[string]*huma.Schema{
				"code_challenge":        {Type: huma.TypeString, Description: "Base64url SHA-256 of the app's code verifier, without padding. The native entry must present the same challenge."},
				"code_challenge_method": {Type: huma.TypeString, Enum: []any{"S256"}},
			}})}
		case "finish-native-provider-link":
			op.Parameters = append(op.Parameters, &huma.Param{Name: "Origin", In: "header", Required: true, Schema: str}, &huma.Param{Name: "X-CSRF-Token", In: "header", Required: true, Schema: str})
			op.RequestBody = &huma.RequestBody{Required: true, Content: jsonBody(&huma.Schema{Type: huma.TypeObject, AdditionalProperties: false, Required: []string{"code", "code_verifier", "redirect_uri"}, Properties: map[string]*huma.Schema{
				"code":          {Type: huma.TypeString, WriteOnly: true, Description: "The one-time link code from the return target. It expires after one minute and is consumed by the first attempt with a valid session."},
				"code_verifier": {Type: huma.TypeString, WriteOnly: true, Description: "The PKCE verifier: 43 to 128 unreserved characters."},
				"redirect_uri":  {Type: huma.TypeString, Description: "The return target used to begin the link."},
			}})}
		case "complete-native-browser-login":
			op.Parameters = append(op.Parameters, &huma.Param{Name: "Origin", In: "header", Required: true, Schema: str}, &huma.Param{Name: "X-CSRF-Token", In: "header", Required: true, Schema: str})
			op.RequestBody = &huma.RequestBody{Required: true, Content: jsonBody(&huma.Schema{Type: huma.TypeObject, AdditionalProperties: false, Required: []string{"state"}, Properties: map[string]*huma.Schema{"state": str}})}
		case "finish-native-provider-login":
			op.Parameters = append(op.Parameters, &huma.Param{Name: "Origin", In: "header", Required: true, Schema: str})
			op.RequestBody = &huma.RequestBody{Required: true, Content: jsonBody(&huma.Schema{Type: huma.TypeObject, AdditionalProperties: false, Required: []string{"code", "code_verifier", "redirect_uri"}, Properties: map[string]*huma.Schema{
				"code":          {Type: huma.TypeString, WriteOnly: true, Description: "The one-time code from the return target. It expires after one minute and is consumed by the first attempt."},
				"code_verifier": {Type: huma.TypeString, WriteOnly: true, Description: "The PKCE verifier: 43 to 128 unreserved characters."},
				"redirect_uri":  {Type: huma.TypeString, Description: "The return target used to begin the sign-in."},
			}})}
		case "complete-oidc-login":
			op.Parameters = append(op.Parameters, &huma.Param{Name: "state", In: "query", Required: true, Schema: str}, &huma.Param{Name: "code", In: "query", Required: true, Schema: str})
		case "complete-saml-login":
			op.RequestBody = &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/x-www-form-urlencoded": {Schema: &huma.Schema{Type: huma.TypeObject, Required: []string{"RelayState", "SAMLResponse"}, Properties: map[string]*huma.Schema{"RelayState": str, "SAMLResponse": {Type: huma.TypeString, WriteOnly: true}}}}}}
		}

		for _, status := range []string{"400", "401", "403", "429", "500"} {
			if op.Responses[status] != nil {
				continue
			}

			op.Responses[status] = &huma.Response{Description: "Structured authentication boundary failure", Content: problem}
		}

		api.OpenAPI().AddOperation(op)
	}
}

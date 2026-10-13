package account

import (
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

type tokenListResponse struct {
	Tokens []identity.PersonalToken `json:"tokens"`
	Policy identity.TokenPolicy     `json:"policy"`
}

type auditResponse struct {
	Events []identity.AuditEvent `json:"events"`
}

type accountUser struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	InstanceAdmin bool   `json:"instance_admin"`
}
type accountState struct {
	PasskeysAvailable     bool             `json:"passkeys_available"`
	RecoveryAuthenticated bool             `json:"recovery_authenticated"`
	AuthEnabled           bool             `json:"auth_enabled"`
	User                  *accountUser     `json:"user"`
	CSRFToken             string           `json:"csrf_token"`
	Libraries             []access.Library `json:"libraries"`
	ActiveLibraryID       string           `json:"active_library_id"`
}
type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password" writeOnly:"true"`
}
type loginResponse struct {
	User      accountUser `json:"user"`
	CSRFToken string      `json:"csrf_token"`
}
type (
	selectLibraryRequest struct {
		LibraryID string `json:"library_id"`
	}
	passwordRequest struct {
		CurrentPassword string `json:"current_password" writeOnly:"true"`
		NewPassword     string `json:"new_password" writeOnly:"true"`
	}
)

type deleteAccountRequest struct {
	Password     string `json:"password,omitempty" writeOnly:"true"`
	Confirmation string `json:"confirmation" enum:"DELETE"`
}
type accountSession struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	AuthMethod string    `json:"auth_method"`
	Current    bool      `json:"current"`
}
type (
	sessionsResponse struct {
		Sessions []accountSession `json:"sessions"`
	}
	managedUser struct {
		accountUser
		Disabled bool `json:"disabled"`
	}
)

type (
	usersResponse struct {
		Users []managedUser `json:"users"`
	}
	createUserRequest struct {
		Login       string      `json:"login"`
		DisplayName string      `json:"display_name"`
		Password    string      `json:"password" writeOnly:"true"`
		LibraryID   string      `json:"library_id,omitempty"`
		Role        access.Role `json:"role,omitempty" enum:"viewer,editor,owner"`
	}
)

type updateUserRequest struct {
	Disabled *bool  `json:"disabled,omitempty"`
	Password string `json:"password,omitempty" writeOnly:"true"`
}
type (
	librariesResponse struct {
		Libraries []access.Library `json:"libraries"`
	}
	createLibraryRequest struct {
		Name string `json:"name"`
	}
	deleteLibraryRequest struct {
		Confirmation string `json:"confirmation"`
	}
	membersResponse struct {
		Members []access.Member `json:"members"`
	}
	setMemberRequest struct {
		Role access.Role `json:"role" enum:"viewer,editor,owner"`
	}
	recoverLibraryRequest struct {
		UserID string `json:"user_id"`
	}
	accountProblem struct {
		Code   string `json:"code"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Type   string `json:"type"`
		Detail string `json:"detail"`
	}
)

// accountAuthorityNote is appended to every cookie/session route rather than
// repeated in each row, because it describes the same boundary for all of them.
const accountAuthorityNote = "Uses the persisted local-mode user when authentication is disabled. Otherwise an authenticated browser session is required, except for the current account and sign-in. Modifying authenticated requests require the current X-CSRF-Token. Credentials and responses must not be cached."

// RegisterOpenAPI describes the directly routed cookie/session operations without
// registering duplicate HTTP handlers. Local mode uses a persisted principal;
// authenticated mutations require X-CSRF-Token in addition to the HttpOnly cookie.
func RegisterOpenAPI(api huma.API) {
	RegisterPasskeyRecoveryOpenAPI(api)
	registerProvisioningOpenAPI(api)

	// The summary is a short sidebar title; detailed documentation belongs in
	// the route description.
	for _, route := range []struct {
		method, path, id, tag, summary, detail string
		request, response                      reflect.Type
	}{
		{http.MethodGet, "/api/v1/account/tokens", "list-account-tokens", "API tokens", "List API tokens", "Returns safe metadata and the instance token policy, never a token secret.", nil, reflect.TypeFor[tokenListResponse]()},
		{http.MethodPost, "/api/v1/account/tokens", "create-account-token", "API tokens", "Create an API token", "The secret appears once in this response and cannot be retrieved again.", reflect.TypeFor[identity.TokenRequest](), reflect.TypeFor[identity.TokenGrant]()},
		{http.MethodDelete, "/api/v1/account/tokens/{id}", "revoke-account-token", "API tokens", "Revoke an API token", "Takes effect immediately; the token is not reissued.", nil, nil},
		{http.MethodGet, "/api/v1/account/audit", "list-audit-events", "Account", "List audit events", "Instance history for an administrator, or owned-library history for a library owner.", nil, reflect.TypeFor[auditResponse]()},
		{http.MethodGet, "/api/v1/account", "get-current-account", "Account", "Get the current account", "Includes the libraries this account can reach.", nil, reflect.TypeFor[accountState]()},
		{http.MethodPost, "/api/v1/account/login", "login-account", "Sessions", "Sign in", "Authenticates a local account and rotates the browser session.", reflect.TypeFor[loginRequest](), reflect.TypeFor[loginResponse]()},
		{http.MethodPost, "/api/v1/account/logout", "logout-account", "Sessions", "Sign out", "Revokes the current browser session.", nil, nil},
		{http.MethodPost, "/api/v1/account/library", "select-current-library", "Account", "Select a library", "Chooses which accessible library this browser acts in.", reflect.TypeFor[selectLibraryRequest](), nil},
		{http.MethodPut, "/api/v1/account/password", "change-account-password", "Account", "Change the password", "Requires the current password. Revokes every session of the account, including the one that made the request, so the client signs in again.", reflect.TypeFor[passwordRequest](), nil},
		{http.MethodDelete, "/api/v1/account", "delete-current-account", "Account", "Delete the current account", "Requires explicit confirmation. Shared libraries survive.", reflect.TypeFor[deleteAccountRequest](), nil},
		{http.MethodGet, "/api/v1/account/sessions", "list-account-sessions", "Sessions", "List sessions", "Active browser sessions owned by this account.", nil, reflect.TypeFor[sessionsResponse]()},
		{http.MethodDelete, "/api/v1/account/sessions", "revoke-other-account-sessions", "Sessions", "Revoke other sessions", "Keeps the session making the request.", nil, nil},
		{http.MethodDelete, "/api/v1/account/sessions/{id}", "revoke-account-session", "Sessions", "Revoke a session", "The session must belong to the current account.", nil, nil},
		{http.MethodGet, "/api/v1/users", "list-users", "Users", "List users", "Instance administration.", nil, reflect.TypeFor[usersResponse]()},
		{http.MethodPost, "/api/v1/users", "create-user", "Users", "Create a user", "Creates an ordinary local account. Instance administration.", reflect.TypeFor[createUserRequest](), reflect.TypeFor[accountUser]()},
		{http.MethodPut, "/api/v1/users/{id}", "update-user", "Users", "Update a user", "Disables an account or resets its password. Instance administration.", reflect.TypeFor[updateUserRequest](), nil},
		{http.MethodGet, "/api/v1/libraries", "list-libraries", "Libraries", "List libraries", "Libraries reachable through membership.", nil, reflect.TypeFor[librariesResponse]()},
		{http.MethodPost, "/api/v1/libraries", "create-library", "Libraries", "Create a library", "The current account becomes its owner.", reflect.TypeFor[createLibraryRequest](), reflect.TypeFor[access.Library]()},
		{http.MethodDelete, "/api/v1/libraries/{id}", "delete-library", "Libraries", "Delete a library", "Requires explicit confirmation and library ownership.", reflect.TypeFor[deleteLibraryRequest](), nil},
		{http.MethodGet, "/api/v1/libraries/{id}/members", "list-library-members", "Library members", "List library members", "Library ownership required.", nil, reflect.TypeFor[membersResponse]()},
		{http.MethodPut, "/api/v1/libraries/{id}/members/{user}", "set-library-member", "Library members", "Set a library member", "Assigns a manual membership role. Library ownership required.", reflect.TypeFor[setMemberRequest](), nil},
		{http.MethodDelete, "/api/v1/libraries/{id}/members/{user}", "remove-library-member", "Library members", "Remove a library member", "Removes a manual membership. Library ownership required.", nil, nil},
		{http.MethodPost, "/api/v1/libraries/{id}/recover", "recover-library-owner", "Library members", "Recover library ownership", "Assigns an owner through audited administrator recovery.", reflect.TypeFor[recoverLibraryRequest](), nil},
	} {
		document := api.OpenAPI()

		operation := &huma.Operation{Method: route.method, Path: route.path, OperationID: route.id, Summary: route.summary, Tags: []string{route.tag}, Description: route.detail + " " + accountAuthorityNote}
		for segment := range strings.SplitSeq(route.path, "/") {
			if strings.HasPrefix(segment, "{") {
				operation.Parameters = append(operation.Parameters, &huma.Param{Name: strings.Trim(segment, "{}"), In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString}})
			}
		}

		if route.method != http.MethodGet && route.id != "login-account" {
			operation.Parameters = append(operation.Parameters, &huma.Param{Name: "X-CSRF-Token", In: "header", Description: "Required with an authenticated browser session; read from current-account.", Schema: &huma.Schema{Type: huma.TypeString}})
		}

		if route.id == "list-audit-events" {
			for _, name := range []string{"library", "actor", "category", "target", "since", "until", "before"} {
				operation.Parameters = append(operation.Parameters, &huma.Param{Name: name, In: "query", Description: "Optional filter; times are Unix seconds and before is an exclusive event ID cursor. Results contain at most 100 events, newest first.", Schema: &huma.Schema{Type: huma.TypeString}})
			}
		}

		if route.request != nil {
			operation.RequestBody = &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: document.Components.Schemas.Schema(route.request, true, route.request.Name())}}}
		}

		response := &huma.Schema{Type: huma.TypeObject, Properties: map[string]*huma.Schema{}, AdditionalProperties: false}
		if route.response != nil {
			response = document.Components.Schemas.Schema(route.response, true, route.response.Name())
		}

		operation.Responses = map[string]*huma.Response{"200": {Description: "Operation completed.", Content: map[string]*huma.MediaType{"application/json": {Schema: response}}}, "default": {Description: "Stable account problem code; authentication, authority, validation or lifecycle conflict.", Content: map[string]*huma.MediaType{"application/problem+json": {Schema: document.Components.Schemas.Schema(reflect.TypeFor[accountProblem](), true, "AccountProblem")}}}}
		document.AddOperation(operation)
	}

	// user is null without a session. Huma does not make a schema reference
	// nullable because generators handle anyOf with null poorly, so the
	// property inlines AccountUser with type [object, null]. Like other
	// response objects, it permits additive fields.
	schemas := api.OpenAPI().Components.Schemas.Map()
	user := *schemas["AccountUser"]
	user.Nullable = true
	user.AdditionalProperties = true
	schemas["AccountState"].Properties["user"] = &user
}

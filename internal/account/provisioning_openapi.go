package account

import (
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/savorum/savorum/internal/access"
	"github.com/savorum/savorum/internal/identity"
)

type invitationCreateRequest struct {
	Role access.Role `json:"role" enum:"viewer,editor,owner"`
	Hint string      `json:"hint,omitempty"`
	Days int         `json:"days,omitempty" minimum:"1" maximum:"30"`
}
type invitationCreateResponse struct {
	Invitation identity.Invitation `json:"invitation"`
	Token      string              `json:"token"`
}
type invitationAcceptRequest struct {
	Token string `json:"token" writeOnly:"true"`
}
type invitationRegisterRequest struct {
	Token       string `json:"token" writeOnly:"true"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password" writeOnly:"true"`
}
type invitationListResponse struct {
	Invitations []identity.Invitation `json:"invitations"`
}
type signInMethodsResponse struct {
	Methods []identity.SignInMethod `json:"methods"`
}
type groupMappingsResponse struct {
	Mappings []identity.GroupMapping `json:"mappings"`
}
type providerDependenciesResponse struct {
	Users []identity.User `json:"users"`
}
type revokedResponse struct {
	Revoked bool `json:"revoked"`
}
type acceptedResponse struct {
	Accepted bool `json:"accepted"`
}
type removedResponse struct {
	Removed bool `json:"removed"`
}

func registerProvisioningOpenAPI(api huma.API) {
	// Summaries are short sidebar titles written out, not derived from IDs.
	for _, r := range []struct {
		method, path, id, tag, summary, detail string
		request, response                      reflect.Type
	}{
		{"GET", "/api/v1/libraries/{id}/invitations", "list-library-invitations", "Invitations", "List invitations", "Metadata and accepted or revoked state, never the secret.", nil, reflect.TypeFor[invitationListResponse]()},
		{"POST", "/api/v1/libraries/{id}/invitations", "create-library-invitation", "Invitations", "Create an invitation", "Returns the link secret once. Library ownership or instance administration required.", reflect.TypeFor[invitationCreateRequest](), reflect.TypeFor[invitationCreateResponse]()},
		{"DELETE", "/api/v1/account/invitations/{id}", "revoke-library-invitation", "Invitations", "Revoke an invitation", "A revoked link cannot be reused.", nil, reflect.TypeFor[revokedResponse]()},
		{"POST", "/api/v1/account/invitations/accept", "accept-library-invitation", "Invitations", "Accept an invitation", "Grants only the invited library role, never instance administration.", reflect.TypeFor[invitationAcceptRequest](), reflect.TypeFor[acceptedResponse]()},
		{"POST", "/api/v1/account/invitations/register", "register-invited-local-account", "Invitations", "Register from an invitation", "Creates the account and consumes the invitation in one transaction.", reflect.TypeFor[invitationRegisterRequest](), reflect.TypeFor[loginResponse]()},
		{"GET", "/api/v1/account/methods", "list-sign-in-methods", "Sign-in methods", "List sign-in methods", "The methods attached to the current account.", nil, reflect.TypeFor[signInMethodsResponse]()},
		{"DELETE", "/api/v1/account/methods/{id}", "unlink-sign-in-method", "Sign-in methods", "Unlink a sign-in method", "Refused when it would leave no usable sign-in or recovery method.", nil, reflect.TypeFor[removedResponse]()},
		{"GET", "/api/v1/users/{id}/methods", "list-managed-user-sign-in-methods", "Sign-in methods", "List a user's sign-in methods", "Instance administration.", nil, reflect.TypeFor[signInMethodsResponse]()},
		{"DELETE", "/api/v1/users/{id}/methods/{method}", "force-unlink-sign-in-method", "Sign-in methods", "Force-unlink a sign-in method", "Audited administrator action for a compromised identity.", nil, reflect.TypeFor[removedResponse]()},
		{"GET", "/api/v1/account/providers/{id}/dependencies", "list-provider-dependent-users", "Sign-in methods", "List provider dependencies", "The users whose sign-in depends on a provider. Check before disabling one.", nil, reflect.TypeFor[providerDependenciesResponse]()},
		{"GET", "/api/v1/account/group-mappings", "list-provider-group-mappings", "Authentication", "List group mappings", "Instance administration.", nil, reflect.TypeFor[groupMappingsResponse]()},
		{"POST", "/api/v1/account/group-mappings", "set-provider-group-mapping", "Authentication", "Set a group mapping", "Maps one provider group value to a library role. Instance administration.", reflect.TypeFor[identity.GroupMapping](), reflect.TypeFor[identity.GroupMapping]()},
		{"DELETE", "/api/v1/account/group-mappings/{id}", "remove-provider-group-mapping", "Authentication", "Remove a group mapping", "Immediately removes the grants that rule produced.", nil, reflect.TypeFor[removedResponse]()},
	} {
		doc := api.OpenAPI()

		op := &huma.Operation{Method: r.method, Path: r.path, OperationID: r.id, Tags: []string{r.tag}, Summary: r.summary, Description: r.detail + " Browser session and CSRF protection are required for mutations, except invited local registration. Invitation secrets are returned once and must never be logged."}
		for part := range strings.SplitSeq(r.path, "/") {
			if strings.HasPrefix(part, "{") {
				op.Parameters = append(op.Parameters, &huma.Param{Name: strings.Trim(part, "{}"), In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString}})
			}
		}

		if r.id == "register-invited-local-account" {
			op.Parameters = append(op.Parameters, &huma.Param{Name: "Origin", In: "header", Required: true, Description: "Must be the configured public origin.", Schema: &huma.Schema{Type: huma.TypeString}})
		}

		if r.request != nil {
			op.RequestBody = &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{"application/json": {Schema: doc.Components.Schemas.Schema(r.request, true, r.request.Name())}}}
		}

		schema := &huma.Schema{Type: huma.TypeObject}
		if r.response != nil {
			schema = doc.Components.Schemas.Schema(r.response, true, r.response.Name())
		}

		op.Responses = map[string]*huma.Response{
			"200":     {Description: "Completed", Content: map[string]*huma.MediaType{"application/json": {Schema: schema}}},
			"default": {Description: "Stable account problem code; authentication, authority, validation or lifecycle conflict.", Content: map[string]*huma.MediaType{"application/problem+json": {Schema: doc.Components.Schemas.Schema(reflect.TypeFor[accountProblem](), true, "AccountProblem")}}},
		}
		doc.AddOperation(op)
	}
}

package identity

// AuthorizationRequest binds browser approval to a client and a one-time exchange.
type AuthorizationRequest struct {
	ID          string `json:"-"`
	UserCode    string `json:"user_code"`
	ClientID    string `json:"client_id"`
	Name        string `json:"client_name"`
	RedirectURI string `json:"-"`
	Challenge   string `json:"-"`
	State       string `json:"-"`
	Resource    string `json:"-"`
	Scope       string `json:"scope"`
	Kind        string `json:"-"`
	ExpiresAt   int64  `json:"expires_at"`
	TokenID     string `json:"-"`
	Status      string `json:"-"`
}

// Connection is a renewable, user-approved grant, without credential material.
type Connection struct {
	PersonalToken
	ClientID string `json:"client_id"`
}

// ConnectionCredentials are delivered only to the authorizing client.
type ConnectionCredentials struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
	LibraryID    string `json:"library_id"`
}

// OAuthClient contains registered public-client metadata. Redirects match exactly.
type OAuthClient struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

// ConnectionDays is the fixed lifetime of a remembered assistant grant.
func (p TokenPolicy) ConnectionDays() int {
	days := p.DefaultDays
	if days <= 0 {
		days = 90
	}

	if p.MaximumDays > 0 && days > p.MaximumDays {
		days = p.MaximumDays
	}

	return days
}

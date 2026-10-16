package connect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func controlCredential(ctx context.Context, setup Setup, action string, client *http.Client) (string, error) {
	if action != "claim" && action != "revoke" {
		return "", errors.New("invalid Connect credential action")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, setup.Relay+"/connect/v1/agent/"+setup.Instance+"/"+action, nil)
	if err != nil {
		return "", fmt.Errorf("prepare Connect credential request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+setup.Credential)

	if client == nil {
		client = &http.Client{Transport: &http.Transport{Proxy: nil}}
	}

	boundedClient := *client
	boundedClient.Timeout = 5 * time.Second
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	response, err := boundedClient.Do(request) // #nosec G704 -- The administrator provides a validated HTTPS relay origin.
	if err != nil {
		return "", fmt.Errorf("contact Connect relay: %w", err)
	}
	defer response.Body.Close()

	if action == "revoke" {
		if response.StatusCode == http.StatusNoContent || response.StatusCode == http.StatusUnauthorized {
			return "", nil
		}

		return "", fmt.Errorf("revoke Connect credential: relay returned %d", response.StatusCode)
	}

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claim Connect setup: relay returned %d", response.StatusCode)
	}

	var result struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 256)).Decode(&result); err != nil {
		return "", fmt.Errorf("read claimed Connect credential: %w", err)
	}

	secret, err := base64.RawURLEncoding.DecodeString(result.Credential)
	if err != nil || len(secret) != 32 || strings.ContainsAny(result.Credential, "\r\n") {
		return "", errors.New("relay returned an invalid Connect credential")
	}

	return result.Credential, nil
}

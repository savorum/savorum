package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

const (
	defaultHealthURL   = "http://127.0.0.1:8080/api/v1/health"
	healthcheckTimeout = 5 * time.Second
)

func newHealthcheckCommand() *cobra.Command {
	var target string

	command := &cobra.Command{
		Use:   "healthcheck",
		Short: "Probe a running instance for readiness",
		Long: "Healthcheck succeeds when the instance answers its readiness endpoint\n" +
			"directly. It ignores proxy environment variables and refuses redirects, so\n" +
			"it describes this process rather than whatever a proxy would answer.",
		Example: "  savorum healthcheck\n  savorum healthcheck --url http://127.0.0.1:8080/api/v1/health",
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return healthcheck(cmd.Context(), target)
		},
	}

	command.Flags().StringVar(&target, "url", defaultHealthURL, "HTTP readiness URL")

	return command
}

func healthcheck(parent context.Context, target string) error {
	// The client's timeout already bounds the probe; the context makes the same
	// deadline observable to the request itself.
	ctx, cancel := context.WithTimeout(parent, healthcheckTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("construct health request: %w", err)
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return errors.New("default HTTP transport has an unexpected type")
	}

	transport := defaultTransport.Clone()
	// A readiness check must describe this process, not a configured outbound
	// proxy. This also makes the image probe independent of proxy environment.
	transport.Proxy = nil
	client := &http.Client{
		Transport: transport,
		Timeout:   healthcheckTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("health endpoint redirected")
		},
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request health endpoint: %w", err)
	}
	defer response.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4*1024))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}

	return nil
}

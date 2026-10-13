package dnssd_test

import (
	"testing"
	"time"

	"github.com/savorum/savorum/internal/dnssd"
)

func TestStartValidation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		opts    dnssd.Options
		wantErr bool
	}{
		{
			name:    "invalid port zero",
			opts:    dnssd.Options{Port: 0, InstanceID: "01J00000000000000000000000"},
			wantErr: true,
		},
		{
			name:    "invalid port negative",
			opts:    dnssd.Options{Port: -1, InstanceID: "01J00000000000000000000000"},
			wantErr: true,
		},
		{
			name:    "invalid port too high",
			opts:    dnssd.Options{Port: 70000, InstanceID: "01J00000000000000000000000"},
			wantErr: true,
		},
		{
			name:    "empty instance ID",
			opts:    dnssd.Options{Port: 8080, InstanceID: ""},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := dnssd.Start(tc.opts)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Start() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestAdvertiserLifecycle(t *testing.T) {
	t.Parallel()

	adv, err := dnssd.Start(dnssd.Options{
		Name:       "Test Savorum",
		Port:       58080,
		InstanceID: "01J1234567890ABCDEFGHJKMNP",
	})
	if err != nil {
		t.Skipf("mDNS socket unavailable in this environment: %v", err)
	}

	// zeroconf registers its serving goroutines with a WaitGroup after Register
	// returns, so an immediate Shutdown races that registration.
	time.Sleep(200 * time.Millisecond)

	adv.Shutdown()
	// Second shutdown call should be idempotent and not panic
	adv.Shutdown()
}

package app

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	api "github.com/savorum/savorum/internal/api/v1"
	"github.com/savorum/savorum/internal/diskspace"
	"github.com/savorum/savorum/internal/storagebudget"
)

func hostingSupportURL(issuer string) string {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || strings.ContainsAny(issuer, "\r\n") {
		return ""
	}

	parsed.Path = "/cloud/account/manage"
	parsed.RawPath = ""

	return parsed.String()
}

func registerStorageStatus(definition huma.API, store Store, options Options) {
	var physicalMu sync.Mutex

	physicalWarning := false

	api.RegisterStorageStatus(definition, func(ctx context.Context) api.CustomerStorageStatus {
		report := api.CustomerStorageStatus{Status: options.DurableBudget.Status(ctx)}
		if options.StoragePath != "" {
			if space, err := diskspace.Inspect(options.StoragePath); err == nil {
				if report.AvailableBytes == nil || space.AvailableBytes < *report.AvailableBytes {
					report.AvailableBytes = &space.AvailableBytes
				}

				if space.TotalInodes != 0 && (report.AvailableInodes == nil || space.AvailableInodes < *report.AvailableInodes) {
					report.AvailableInodes = &space.AvailableInodes
				}

				probeCtx, cancel := context.WithTimeout(ctx, time.Second)
				footprint, footprintErr := store.StorageFootprint(probeCtx)

				cancel()

				physicalMu.Lock()

				physicalState, warning := storagebudget.PhysicalState(space, footprint, physicalWarning)
				if footprintErr != nil && physicalState != "limited" {
					physicalState, warning = "unknown", physicalWarning
				}

				physicalWarning = warning
				physicalMu.Unlock()

				report.State = combineStorageState(report.State, physicalState, options.DurableBudget == nil)
			} else {
				report.State = "unknown"
			}
		}

		if tenant, err := store.HostingTenant(ctx); err == nil {
			report.Hosted = true
			report.SupportURL = hostingSupportURL(tenant.Issuer)
		}

		return report
	})
}

func combineStorageState(logical, physical string, selfHosted bool) string {
	if physical == "limited" || logical == "limited" {
		return "limited"
	}

	if physical == "unknown" || (!selfHosted && logical == "unknown") {
		return "unknown"
	}

	if physical == "warning" || logical == "warning" {
		return "warning"
	}

	return "normal"
}

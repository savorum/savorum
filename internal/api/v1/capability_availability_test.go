package v1

import (
	"testing"

	"github.com/savorum/savorum/internal/importer"
)

type advertisedExchange struct{ ExchangeService }

func (advertisedExchange) Formats() []importer.Format {
	return []importer.Format{{ID: "text"}, {ID: "document"}}
}

func (advertisedExchange) AvailableFormats() []importer.Format {
	return []importer.Format{{ID: "text"}}
}

type legacyAdvertisedExchange struct{ ExchangeService }

func (legacyAdvertisedExchange) Formats() []importer.Format { return []importer.Format{{ID: "future"}} }

func TestCapabilitiesRuntimeFormatAdvertisement(t *testing.T) {
	value := authenticatedCapabilities(Capabilities{}, Options{Exchange: advertisedExchange{}}, 0)
	if len(value.Importers) != 1 || value.Importers[0].ID != "text" {
		t.Fatal(value.Importers)
	}

	value = authenticatedCapabilities(Capabilities{}, Options{Exchange: legacyAdvertisedExchange{}}, 0)
	if len(value.Importers) != 1 || value.Importers[0].ID != "future" {
		t.Fatal("legacy adapter compatibility", value.Importers)
	}
}

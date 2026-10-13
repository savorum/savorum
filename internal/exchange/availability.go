package exchange

import "github.com/savorum/savorum/internal/importer"

// AvailableFormats advertises runtime-supported source types for discovery.
func (s *Service) AvailableFormats() []importer.Format { return s.registry.AvailableFormats() }

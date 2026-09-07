package planner

import (
	"context"

	"travel-agent/backend-go/internal/domain"
)

// POIEnricher is implemented by the map provider. Enrichment is deliberately
// optional so planning remains available when map credentials are absent.
type POIEnricher interface {
	EnrichPOIs(context.Context, string, []domain.POI) (domain.ItineraryPOIs, error)
}

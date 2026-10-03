// Package sources defines the common interface of earnings sources
// (Uber, FREE NOW, Bolt) and helpers for reading downloaded files.
package sources

import (
	"context"

	"weekpay/internal/model"
)

// Data is what a source returns for one week.
type Data struct {
	Earnings []model.DriverEarning
	// Raw is the source table as downloaded, for the source's sheet in the report.
	Raw model.Table
	// Origin describes where the data came from (file path or API).
	Origin   string
	Warnings []string
}

// Source fetches drivers' weekly earnings from one platform.
type Source interface {
	Name() string
	Fetch(ctx context.Context, w model.Week) (*Data, error)
}

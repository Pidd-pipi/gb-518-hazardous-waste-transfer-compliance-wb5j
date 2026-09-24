package service

import (
	"context"
	"time"

	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/model"
	"github.com/blueship581/hazardous-waste-transfer-compliance/backend/internal/repository"
)

// quotaService computes read-only annual quota projections. Occupancy is the
// sum of submitted, in_transit and received manifest weight grouped by
// generator code and the UTC natural year of the manifest effective time.
type quotaService struct {
	manifests  repository.TransferManifestRepository
	generators repository.WasteGeneratorRepository
}

func newQuotaService(manifests repository.TransferManifestRepository, generators repository.WasteGeneratorRepository) *quotaService {
	return &quotaService{manifests: manifests, generators: generators}
}

type generatorYear struct {
	code string
	year int
}

// occupancyIndex loads all occupied weights once and indexes them by
// generator code and year.
func (q *quotaService) occupancyIndex(ctx context.Context) (map[generatorYear]float64, error) {
	rows, err := q.manifests.SumOccupiedWeights(ctx)
	if err != nil {
		return nil, err
	}
	index := make(map[generatorYear]float64, len(rows))
	for _, row := range rows {
		index[generatorYear{code: row.GeneratorCode, year: row.Year}] = row.TotalKg
	}
	return index, nil
}

func (q *quotaService) projection(generator model.WasteGenerator, year int, used float64) *model.QuotaUsage {
	usage := &model.QuotaUsage{
		GeneratorCode: generator.Code,
		Year:          year,
		AnnualQuotaKg: generator.AnnualQuotaKg,
		UsedKg:        used,
		Limited:       generator.AnnualQuotaKg > 0,
	}
	if usage.Limited {
		usage.RemainingKg = generator.AnnualQuotaKg - used
	}
	return usage
}

// decorateGenerator attaches quota usage for the requested year (or the
// current natural year).
func (q *quotaService) decorateGenerator(ctx context.Context, generator *model.WasteGenerator, year int) error {
	if year == 0 {
		year = time.Now().UTC().Year()
	}
	index, err := q.occupancyIndex(ctx)
	if err != nil {
		return err
	}
	generator.QuotaUsage = q.projection(*generator, year, index[generatorYear{code: generator.Code, year: year}])
	return nil
}

// decorateGenerators attaches current-year quota usage to a page of generators.
func (q *quotaService) decorateGenerators(ctx context.Context, generators []model.WasteGenerator) error {
	if len(generators) == 0 {
		return nil
	}
	index, err := q.occupancyIndex(ctx)
	if err != nil {
		return err
	}
	year := time.Now().UTC().Year()
	for i := range generators {
		generators[i].QuotaUsage = q.projection(generators[i], year, index[generatorYear{code: generators[i].Code, year: year}])
	}
	return nil
}

// decorateManifest attaches the linked generator quota usage for the natural
// year of the manifest effective time.
func (q *quotaService) decorateManifest(ctx context.Context, manifest *model.TransferManifest) error {
	generator, err := q.generators.FindByCode(ctx, manifest.GeneratorCode)
	if err != nil {
		// Quota projection is supplemental; a missing linked generator must
		// not hide the manifest itself.
		return nil
	}
	index, err := q.occupancyIndex(ctx)
	if err != nil {
		return err
	}
	year := manifest.EffectiveAt.UTC().Year()
	manifest.QuotaUsage = q.projection(generator, year, index[generatorYear{code: generator.Code, year: year}])
	return nil
}

// decorateManifests batch-loads generators once and attaches per-manifest quota
// usage for each manifest's own effective-at year.
func (q *quotaService) decorateManifests(ctx context.Context, manifests []model.TransferManifest) error {
	if len(manifests) == 0 {
		return nil
	}
	index, err := q.occupancyIndex(ctx)
	if err != nil {
		return err
	}
	generators := make(map[string]model.WasteGenerator, len(manifests))
	for i := range manifests {
		code := manifests[i].GeneratorCode
		if _, loaded := generators[code]; !loaded {
			generator, findErr := q.generators.FindByCode(ctx, code)
			if findErr != nil {
				continue
			}
			generators[code] = generator
		}
		generator, ok := generators[code]
		if !ok {
			continue
		}
		year := manifests[i].EffectiveAt.UTC().Year()
		manifests[i].QuotaUsage = q.projection(generator, year, index[generatorYear{code: code, year: year}])
	}
	return nil
}

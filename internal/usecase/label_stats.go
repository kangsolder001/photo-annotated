package usecase

import (
	"context"
	"math"
)

// LabelStatShare: angka satu label beserta porsinya dari semua kotak berlabel.
type LabelStatShare struct {
	LabelStat
	Share float64 // 0..1, dibulatkan 3 desimal
}

// LabelStatsResult adalah isi GET /admin/stats/labels (spec-labels-admin §5.9).
type LabelStatsResult struct {
	Totals LabelStatsTotals
	Labels []LabelStatShare
}

// LabelStats menghitung statistik per label untuk melihat keseimbangan dataset.
type LabelStats struct{ repo AdminStatsRepo }

func NewLabelStats(repo AdminStatsRepo) *LabelStats { return &LabelStats{repo: repo} }

// Labels: semua label (termasuk arsip dan yang belum pernah dipakai), urut jumlah kotak menurun.
func (u *LabelStats) Labels(ctx context.Context, f LabelStatsFilter) (*LabelStatsResult, error) {
	tot, stats, err := u.repo.LabelStats(ctx, f)
	if err != nil {
		return nil, err
	}
	labeled := 0
	for _, s := range stats {
		labeled += s.Annotations
	}
	out := &LabelStatsResult{Totals: tot, Labels: make([]LabelStatShare, len(stats))}
	for i, s := range stats {
		out.Labels[i] = LabelStatShare{LabelStat: s}
		if labeled > 0 {
			out.Labels[i].Share = math.Round(float64(s.Annotations)/float64(labeled)*1000) / 1000
		}
	}
	return out, nil
}

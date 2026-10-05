package progress

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarize(t *testing.T) {
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	pageA := PageInfo{ID: uuid.New(), Title: "A", DisplayOrder: 0}
	pageB := PageInfo{ID: uuid.New(), Title: "B", DisplayOrder: 1}
	pageC := PageInfo{ID: uuid.New(), Title: "C", DisplayOrder: 2}

	reached := func(pageID uuid.UUID, at time.Time) ProgressRow {
		return ProgressRow{PageID: pageID, ReachedAt: at}
	}
	completed := func(pageID uuid.UUID, at time.Time) ProgressRow {
		c := at
		return ProgressRow{PageID: pageID, ReachedAt: at, CompletedAt: &c}
	}

	tests := []struct {
		name           string
		pages          []PageInfo
		rows           []ProgressRow
		wantStatus     Status
		wantReached    int32
		wantCompleted  int32
		wantTotal      int32
		wantPercent    int32
		wantHighestIdx int // -1 means nil
	}{
		{
			name:           "empty course",
			pages:          nil,
			rows:           nil,
			wantStatus:     StatusNotStarted,
			wantTotal:      0,
			wantPercent:    0,
			wantHighestIdx: -1,
		},
		{
			name:           "no progress",
			pages:          []PageInfo{pageA, pageB, pageC},
			rows:           nil,
			wantStatus:     StatusNotStarted,
			wantTotal:      3,
			wantPercent:    0,
			wantHighestIdx: -1,
		},
		{
			name:           "partial reached only",
			pages:          []PageInfo{pageA, pageB, pageC},
			rows:           []ProgressRow{reached(pageA.ID, t0), reached(pageB.ID, t0)},
			wantStatus:     StatusInProgress,
			wantReached:    2,
			wantTotal:      3,
			wantPercent:    0,
			wantHighestIdx: 1, // pageB
		},
		{
			name:           "partial completed",
			pages:          []PageInfo{pageA, pageB, pageC},
			rows:           []ProgressRow{completed(pageA.ID, t0), reached(pageB.ID, t0)},
			wantStatus:     StatusInProgress,
			wantReached:    2,
			wantCompleted:  1,
			wantTotal:      3,
			wantPercent:    33, // floor(100/3)
			wantHighestIdx: 1,
		},
		{
			name:           "all completed",
			pages:          []PageInfo{pageA, pageB, pageC},
			rows:           []ProgressRow{completed(pageA.ID, t0), completed(pageB.ID, t0), completed(pageC.ID, t1)},
			wantStatus:     StatusCompleted,
			wantReached:    3,
			wantCompleted:  3,
			wantTotal:      3,
			wantPercent:    100,
			wantHighestIdx: 2,
		},
		{
			name:  "progress for deleted page is ignored",
			pages: []PageInfo{pageA, pageB},
			rows: []ProgressRow{
				completed(pageA.ID, t0),
				completed(uuid.New(), t0),
			},
			wantStatus:     StatusInProgress,
			wantReached:    1,
			wantCompleted:  1,
			wantTotal:      2,
			wantPercent:    50,
			wantHighestIdx: 0,
		},
		{
			name:           "all pages deleted leaves not_started",
			pages:          nil,
			rows:           []ProgressRow{completed(pageA.ID, t0)},
			wantStatus:     StatusNotStarted,
			wantTotal:      0,
			wantPercent:    0,
			wantHighestIdx: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			summary, views := summarize(tc.pages, tc.rows)
			assert.Equal(t, tc.wantStatus, summary.Status)
			assert.Equal(t, tc.wantTotal, summary.TotalPages)
			assert.Equal(t, tc.wantReached, summary.ReachedPages)
			assert.Equal(t, tc.wantCompleted, summary.CompletedPages)
			assert.Equal(t, tc.wantPercent, summary.CompletionPercentage)

			if tc.wantHighestIdx < 0 {
				assert.Nil(t, summary.HighestReachedPage)
			} else {
				require.NotNil(t, summary.HighestReachedPage)
				assert.Equal(t, tc.pages[tc.wantHighestIdx].ID, summary.HighestReachedPage.PageID)
				assert.Equal(t, int32(tc.wantHighestIdx+1), summary.HighestReachedPage.PageNumber)
				assert.Equal(t, tc.pages[tc.wantHighestIdx].Title, summary.HighestReachedPage.Title)
			}

			assert.Len(t, views, len(tc.pages))
			for i, v := range views {
				assert.Equal(t, tc.pages[i].ID, v.PageID)
				assert.Equal(t, int32(i+1), v.PageNumber)
			}
		})
	}
}

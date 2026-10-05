package progress

import "github.com/google/uuid"

// Progress rows for pages absent from `pages` are silently excluded, which is
// how deleted pages drop out of the summary.
func summarize(pages []PageInfo, rows []ProgressRow) (CourseProgressSummary, []PageProgressView) {
	progressByPage := make(map[uuid.UUID]ProgressRow, len(rows))
	for _, r := range rows {
		progressByPage[r.PageID] = r
	}

	views := make([]PageProgressView, 0, len(pages))
	var reached, completed int32
	var highestReachedPage *uuid.UUID
	var highestOrder int32 = -1

	for i, p := range pages {
		pageNumber := int32(i + 1)
		view := PageProgressView{
			PageID:     p.ID,
			PageNumber: pageNumber,
			Title:      p.Title,
		}
		if row, ok := progressByPage[p.ID]; ok {
			reached++
			r := row.ReachedAt
			view.ReachedAt = &r
			if row.CompletedAt != nil {
				completed++
				c := *row.CompletedAt
				view.CompletedAt = &c
			}
			if p.DisplayOrder > highestOrder {
				highestOrder = p.DisplayOrder
				id := p.ID
				highestReachedPage = &id
			}
		}
		views = append(views, view)
	}

	total := int32(len(pages))
	summary := CourseProgressSummary{
		TotalPages:         total,
		ReachedPages:       reached,
		CompletedPages:     completed,
		HighestReachedPage: highestReachedPage,
	}

	if total > 0 {
		summary.CompletionPercentage = completed * 100 / total
	}

	switch {
	case total > 0 && completed == total:
		summary.Status = StatusCompleted
	case reached == 0:
		summary.Status = StatusNotStarted
	default:
		summary.Status = StatusInProgress
	}

	return summary, views
}

package worker

import (
	"os"
)

// EstimateComplexity returns a complexity score for a checklist item.
// Higher scores mean the item will take more agent effort.
// Scoring:
//   - Base: 100 (minimum for any item)
//   - File size: +1 per 100 bytes (larger files = more to read/understand)
//   - Approximate lines: +1 per 10 estimated lines (size/40 as proxy)
//   - Cap at 1000 to prevent a single huge file from consuming entire budget
func EstimateComplexity(item ChecklistItem) int {
	score := 100 // base score

	// Use os.Stat only — avoids reading entire file into memory
	info, err := os.Stat(item.Path)
	if err == nil {
		size := info.Size()
		// File size component
		sizeScore := int(size / 100)
		score += sizeScore

		// Approximate line count: average ~40 bytes/line
		approxLines := int(size / 40)
		score += approxLines / 10
	}

	// Description length suggests complexity
	if len(item.Note) > 50 {
		score += 50
	}

	// Cap at 1000
	if score > 1000 {
		score = 1000
	}

	return score
}

// SelectBatchByComplexity picks items from the pending list up to the
// complexity budget. Always includes at least 1 item even if it exceeds budget.
func SelectBatchByComplexity(pending []ChecklistItem, budget int) []ChecklistItem {
	if len(pending) == 0 {
		return nil
	}

	var batch []ChecklistItem
	totalComplexity := 0

	for _, item := range pending {
		score := EstimateComplexity(item)

		// Always include at least one item
		if len(batch) == 0 {
			batch = append(batch, item)
			totalComplexity += score
			continue
		}

		// Stop if adding this item would exceed budget
		if totalComplexity+score > budget {
			break
		}

		batch = append(batch, item)
		totalComplexity += score
	}

	return batch
}

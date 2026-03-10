package worker

// ShapeChecklistItem converts a checklist item into a schedulable task with an
// explicit claim set when Ralph can determine one safely up front.
func ShapeChecklistItem(item ChecklistItem) Task {
	if item.Path == "" {
		return Task{
			ID:          item.Line,
			Item:        item,
			SerialOnly:  true,
			ShapeReason: "missing path",
		}
	}

	return Task{
		ID:          item.Path,
		Item:        item,
		Files:       []string{item.Path},
		ShapeReason: "exact path",
	}
}

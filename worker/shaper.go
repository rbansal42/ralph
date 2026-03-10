package worker

import "strings"

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
		Files:       shapedFilesForPath(item.Path),
		ShapeReason: "exact path",
	}
}

func shapedFilesForPath(path string) []string {
	files := []string{path}
	if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
		files = append(files, strings.TrimSuffix(path, ".go")+"_test.go")
	}
	return files
}

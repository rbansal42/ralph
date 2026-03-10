package worker

// Task is a schedulable unit of work derived from one checklist item or a
// small compatible bundle.
type Task struct {
	ID         string
	Item       ChecklistItem
	Files      []string
	SerialOnly bool
	ShapeReason string
}

// ClaimTable tracks which child currently owns each claimed file.
type ClaimTable struct {
	owners map[string]string
}

// NewClaimTable creates an empty claim table.
func NewClaimTable() *ClaimTable {
	return &ClaimTable{owners: make(map[string]string)}
}

// TryClaim reserves the provided files for childID if none are currently
// claimed. It returns false when any file is already owned by another child.
func (c *ClaimTable) TryClaim(childID string, files []string) bool {
	for _, file := range files {
		if _, exists := c.owners[file]; exists {
			return false
		}
	}

	for _, file := range files {
		c.owners[file] = childID
	}

	return true
}

// Release drops any claims currently owned by childID.
func (c *ClaimTable) Release(childID string) {
	for file, owner := range c.owners {
		if owner == childID {
			delete(c.owners, file)
		}
	}
}

// PartitionDispatchable separates tasks into parallel and serial lanes. Tasks
// with an unknown or explicitly serial file footprint are kept in the serial lane.
func PartitionDispatchable(tasks []Task) (parallel []Task, serial []Task) {
	for _, task := range tasks {
		if task.SerialOnly || len(task.Files) == 0 {
			serial = append(serial, task)
			continue
		}
		parallel = append(parallel, task)
	}

	return parallel, serial
}

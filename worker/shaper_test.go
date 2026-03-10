package worker

import "testing"

func TestShapeTaskUsesExactPathClaim(t *testing.T) {
	item := ChecklistItem{Path: "app/Tasks/A.php"}

	task := ShapeChecklistItem(item)

	if len(task.Files) != 1 || task.Files[0] != "app/Tasks/A.php" {
		t.Fatalf("Files = %#v, want exact-path claim", task.Files)
	}
	if task.SerialOnly {
		t.Fatal("exact-path task should not be serial-only")
	}
}

func TestShapeTaskFallsBackToSerialWhenNoRuleApplies(t *testing.T) {
	item := ChecklistItem{}

	task := ShapeChecklistItem(item)

	if !task.SerialOnly {
		t.Fatal("expected serial fallback for unshapeable item")
	}
	if task.ShapeReason == "" {
		t.Fatal("expected shaping reason for serial fallback")
	}
}

package worker

// ParentWorker coordinates a bounded pool of child tasks for one logical worker.
type ParentWorker struct {
	parallelism int
	Generation  int
	Completed   map[string]bool
	queue       []Task
	inFlight    map[string]Task
	claims      *ClaimTable
}

// ParentSnapshot captures the compact machine state required to recreate a parent.
type ParentSnapshot struct {
	Generation int
	Completed  map[string]bool
	Queue      []Task
}

// Enqueue appends tasks to the parent queue.
func (p *ParentWorker) Enqueue(tasks ...Task) {
	p.queue = append(p.queue, tasks...)
}

// FillChildSlots dispatches compatible queued tasks until the parent reaches
// its slot limit or runs out of dispatchable work.
func (p *ParentWorker) FillChildSlots() {
	for len(p.inFlight) < p.parallelism {
		task, ok := p.nextDispatchableTask()
		if !ok {
			return
		}
		p.inFlight[task.ID] = task
	}
}

// InFlightCount returns the number of active child tasks.
func (p *ParentWorker) InFlightCount() int {
	return len(p.inFlight)
}

// IsInFlight reports whether the task with id is currently active.
func (p *ParentWorker) IsInFlight(id string) bool {
	_, ok := p.inFlight[id]
	return ok
}

// Queue returns a copy of the queued tasks.
func (p *ParentWorker) Queue() []Task {
	queue := make([]Task, len(p.queue))
	copy(queue, p.queue)
	return queue
}

// MarkInFlight registers a task as active and claims its files.
func (p *ParentWorker) MarkInFlight(task Task) {
	p.claims.TryClaim(task.ID, task.Files)
	p.inFlight[task.ID] = task
}

// CanResetNow reports whether the parent has any active child tasks.
func (p *ParentWorker) CanResetNow() bool {
	return len(p.inFlight) == 0
}

// Snapshot captures the compact machine state needed to recreate the parent.
func (p *ParentWorker) Snapshot() ParentSnapshot {
	completed := make(map[string]bool, len(p.Completed))
	for id, done := range p.Completed {
		completed[id] = done
	}

	return ParentSnapshot{
		Generation: p.Generation,
		Completed:  completed,
		Queue:      p.Queue(),
	}
}

// ParentWorkerFromSnapshot recreates a parent worker from persisted machine state.
func ParentWorkerFromSnapshot(snapshot ParentSnapshot, parallelism int) *ParentWorker {
	parent := &ParentWorker{
		parallelism: parallelism,
		Generation:  snapshot.Generation,
		Completed:   make(map[string]bool, len(snapshot.Completed)),
		queue:       make([]Task, len(snapshot.Queue)),
		inFlight:    make(map[string]Task),
		claims:      NewClaimTable(),
	}

	for id, done := range snapshot.Completed {
		parent.Completed[id] = done
	}
	copy(parent.queue, snapshot.Queue)

	return parent
}

func (p *ParentWorker) nextDispatchableTask() (Task, bool) {
	for idx, task := range p.queue {
		if len(task.Files) == 0 || task.SerialOnly {
			continue
		}
		if !p.claims.TryClaim(task.ID, task.Files) {
			continue
		}

		p.queue = append(p.queue[:idx], p.queue[idx+1:]...)
		return task, true
	}

	return Task{}, false
}

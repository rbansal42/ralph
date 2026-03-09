package worker

// ParentWorker coordinates a bounded pool of child tasks for one logical worker.
type ParentWorker struct {
	parallelism int
	queue       []Task
	inFlight    map[string]Task
	claims      *ClaimTable
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

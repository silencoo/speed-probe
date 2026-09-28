package taskpoll

import "testing"

type observedItem struct {
	exits int
	code  TaskPollExitCode
}

func (i *observedItem) ID() string                     { return "test" }
func (i *observedItem) TaskName() string               { return "test" }
func (i *observedItem) Weight() uint                   { return 1 }
func (i *observedItem) Count() int                     { return 2 }
func (i *observedItem) Yield(int, *TaskPollController) {}
func (i *observedItem) OnExit(code TaskPollExitCode)   { i.exits++; i.code = code }
func (i *observedItem) Init() TaskPollItem             { return i }

func TestCancelKeepsSlotUntilRunningNodeFinishes(t *testing.T) {
	p := NewTaskPollController("test", 1, 0, 0)
	item := &observedItem{}
	p.Push(item)
	_, running := p.populate()
	p.Remove(item.ID(), TPExitInterrupt)
	if item.exits != 0 {
		t.Fatal("released client slot while a node was still running")
	}
	p.release(running)
	if item.exits != 1 || item.code != TPExitInterrupt {
		t.Fatal("missing interruption completion")
	}
}
func TestCancelQueuedTaskReleasesImmediately(t *testing.T) {
	p := NewTaskPollController("test", 1, 0, 0)
	item := &observedItem{}
	p.Push(item)
	p.Remove(item.ID(), TPExitInterrupt)
	if item.exits != 1 {
		t.Fatal("queued task did not release its client slot")
	}
}

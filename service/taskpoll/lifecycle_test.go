package taskpoll

import (
	"testing"
	"time"
)

type callbackItem struct {
	observedItem
	callback func()
}

func (i *callbackItem) OnExit(code TaskPollExitCode) { i.observedItem.OnExit(code); i.callback() }
func TestCompletionCallbackCanReenterScheduler(t *testing.T) {
	p := NewTaskPollController("test", 1, 0, 0)
	done := make(chan struct{})
	item := &callbackItem{callback: func() { p.AwaitingCount(); close(done) }}
	p.Push(item)
	go p.Remove(item.ID(), TPExitInterrupt)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("callback held scheduler lock")
	}
}
func TestCancelAfterLastNodeDispatched(t *testing.T) {
	p := NewTaskPollController("test", 2, 0, 0)
	item := &observedItem{}
	p.Push(item)
	_, first := p.populate()
	_, last := p.populate()
	p.Remove(item.ID(), TPExitInterrupt)
	if item.exits != 0 {
		t.Fatal("running task released early")
	}
	p.release(first)
	p.release(last)
	if item.exits != 1 || item.code != TPExitInterrupt {
		t.Fatal("last dispatched task lost cancellation")
	}
}

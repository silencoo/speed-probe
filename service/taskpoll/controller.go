package taskpoll

import (
	"context"
	"github.com/silencoo/speed-probe/utils"
	"sync"
	"time"
)

type TaskPollExitCode uint

const (
	TPExitSuccess TaskPollExitCode = iota
	TPExitError
	TPExitInterrupt
)

type taskPollItemWrapper struct {
	TaskPollItem
	counter  int
	running  int
	exitCode TaskPollExitCode
	exitOnce sync.Once
}

func (w *taskPollItemWrapper) OnExit(code TaskPollExitCode) {
	w.exitOnce.Do(func() { w.TaskPollItem.OnExit(code) })
}

type TaskPollController struct {
	name        string
	concurrency uint
	interval    time.Duration
	taskPoll    []*taskPollItemWrapper
	active      map[string]*taskPollItemWrapper
	current     uint
	pollLock    sync.Mutex
	wake        chan struct{}
}

func (p *TaskPollController) Name() string { return p.name }
func (p *TaskPollController) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}
func (p *TaskPollController) populate() (int, *taskPollItemWrapper) {
	p.pollLock.Lock()
	defer p.pollLock.Unlock()
	if p.current >= p.concurrency || len(p.taskPoll) == 0 {
		return 0, nil
	}
	w := p.taskPoll[0]
	p.taskPoll = p.taskPoll[1:]
	index := w.counter
	w.counter++
	w.running++
	p.current++
	// Round robin gives each admitted task a turn without random starvation.
	if w.counter < w.Count() {
		p.taskPoll = append(p.taskPoll, w)
	}
	return index, w
}
func (p *TaskPollController) release(w *taskPollItemWrapper) {
	p.pollLock.Lock()
	w.running--
	p.current--
	finished := w.running == 0 && w.counter >= w.Count()
	if finished {
		delete(p.active, w.ID())
	}
	code := w.exitCode
	p.pollLock.Unlock()
	p.signal()
	// Never invoke callbacks (including network I/O) under the scheduler lock.
	if finished {
		w.OnExit(code)
	}
}
func (p *TaskPollController) AwaitingCount() int {
	p.pollLock.Lock()
	defer p.pollLock.Unlock()
	n := 0
	for _, w := range p.taskPoll {
		n += w.Count() - w.counter
	}
	return n
}
func (p *TaskPollController) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-utils.MakeSysChan():
			cancel()
		case <-ctx.Done():
		}
	}()
	p.Run(ctx)
}
func (p *TaskPollController) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		index, w := p.populate()
		if w == nil {
			select {
			case <-ctx.Done():
				return
			case <-p.wake:
			}
			continue
		}
		go func() {
			defer func() {
				if recover() != nil {
					p.Remove(w.ID(), TPExitError)
				}
				p.release(w)
			}()
			w.Yield(index, p)
		}()
		if p.interval > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.interval):
			}
		}
	}
}

func (p *TaskPollController) Push(item TaskPollItem) TaskPollItem {
	w := &taskPollItemWrapper{TaskPollItem: item}
	p.pollLock.Lock()
	if _, exists := p.active[item.ID()]; exists {
		p.pollLock.Unlock()
		panic("duplicate task ID")
	}
	if item.Count() > 0 {
		p.taskPoll = append(p.taskPoll, w)
		p.active[item.ID()] = w
	}
	p.pollLock.Unlock()
	p.signal()
	if item.Count() == 0 {
		w.OnExit(TPExitSuccess)
	}
	return item
}
func (p *TaskPollController) Remove(id string, code TaskPollExitCode) {
	p.pollLock.Lock()
	w := p.active[id]
	if w == nil {
		p.pollLock.Unlock()
		return
	}
	w.exitCode = code
	w.counter = w.Count()
	kept := p.taskPoll[:0]
	for _, other := range p.taskPoll {
		if other != w {
			kept = append(kept, other)
		}
	}
	p.taskPoll = kept
	finished := w.running == 0
	if finished {
		delete(p.active, id)
	}
	p.pollLock.Unlock()
	p.signal()
	if finished {
		w.OnExit(code)
	}
}
func NewTaskPollController(name string, concurrency uint, interval, emptyWait time.Duration) *TaskPollController {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 64 {
		concurrency = 64
	}
	return &TaskPollController{name: name, concurrency: concurrency, interval: interval, active: map[string]*taskPollItemWrapper{}, wake: make(chan struct{}, 1)}
}

package service

import (
	"context"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/service/macros"
	"github.com/silencoo/speed-probe/service/matrices"
	"github.com/silencoo/speed-probe/service/taskpoll"
	"github.com/silencoo/speed-probe/utils"
	"github.com/silencoo/speed-probe/utils/structs"
	"github.com/silencoo/speed-probe/vendors"
	"sync"
	"time"
)

type TestingPollItem struct {
	id        string
	name      string
	ctx       context.Context
	cancel    context.CancelCauseFunc
	request   *interfaces.SlaveRequest
	matrices  []interfaces.SlaveRequestMatrixEntry
	macros    []interfaces.SlaveRequestMacroType
	results   *structs.AsyncArr[interfaces.SlaveEntrySlot]
	onProcess func(*TestingPollItem, int, interfaces.SlaveEntrySlot)
	onExit    func(*TestingPollItem, taskpoll.TaskPollExitCode)
	exitOnce  sync.Once
	authorize func() error
}

func (t *TestingPollItem) ID() string       { return t.id }
func (t *TestingPollItem) TaskName() string { return t.name }
func (t *TestingPollItem) Weight() uint     { return 1 }
func (t *TestingPollItem) Count() int       { return len(t.request.Nodes) }
func (t *TestingPollItem) Yield(idx int, p *taskpoll.TaskPollController) {
	if t.ctx.Err() != nil {
		return
	}
	if t.authorize != nil {
		if err := t.authorize(); err != nil {
			t.cancel(&Failure{"permission_revoked", "Client authorization changed"})
			p.Remove(t.id, taskpoll.TPExitError)
			return
		}
	}
	node := t.request.Nodes[idx]
	result := interfaces.SlaveEntrySlot{Index: idx, ProxyInfo: interfaces.ProxyInfo{Name: node.Name}, InvokeDuration: -1, Matrices: []interfaces.MatrixResponse{}}
	defer func() {
		if recover() != nil {
			t.cancel(&Failure{"execution_failed", "Node execution failed"})
			p.Remove(t.id, taskpoll.TPExitError)
			return
		}
		if t.ctx.Err() != nil {
			return
		}
		t.results.Push(result)
		t.onProcess(t, idx, result)
	}()
	base := vendors.Build(t.ctx, t.request.Vendor, node.Name, node.Payload)
	if closer, ok := base.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	if base.Status() != interfaces.VStatusOperational {
		result.Error = "Node configuration is invalid or unsupported by the selected core"
		return
	}
	proxy := vendors.WithContext(t.ctx, base)
	result.ProxyInfo = proxy.ProxyInfo()
	macroMap := structs.NewAsyncMap[interfaces.SlaveRequestMacroType, interfaces.SlaveRequestMacro]()
	start := time.Now()
	var wg sync.WaitGroup
	for _, kind := range t.macros {
		kind := kind
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if recover() != nil {
					t.cancel(&Failure{"execution_failed", "Test execution failed"})
				}
			}()
			if t.ctx.Err() != nil {
				return
			}
			m := macros.Find(kind)
			if err := m.Run(proxy, t.request); err != nil {
				t.cancel(&Failure{"execution_failed", "Test execution failed"})
				return
			}
			macroMap.Set(kind, m)
		}()
	}
	wg.Wait()
	if t.ctx.Err() != nil {
		return
	}
	result.InvokeDuration = time.Since(start).Milliseconds()
	for _, entry := range t.matrices {
		m := matrices.Find(entry.Type)
		macro := macroMap.MustGet(m.MacroJob())
		if macro == nil {
			continue
		}
		m.Extract(entry, macro)
		result.Matrices = append(result.Matrices, interfaces.MatrixResponse{Type: m.Type(), Payload: utils.ToJSON(m)})
	}
}
func (t *TestingPollItem) OnExit(code taskpoll.TaskPollExitCode) {
	t.exitOnce.Do(func() { t.onExit(t, code) })
}
func (t *TestingPollItem) Init() taskpoll.TaskPollItem {
	t.results = structs.NewAsyncArr[interfaces.SlaveEntrySlot]()
	return t
}

package service

import (
	"context"
	"errors"
	"github.com/gorilla/websocket"
	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/preconfigs"
	"github.com/silencoo/speed-probe/service/matrices"
	"github.com/silencoo/speed-probe/service/taskpoll"
	"github.com/silencoo/speed-probe/utils"
	"github.com/silencoo/speed-probe/vendors"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

var upgrader = websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024, HandshakeTimeout: 10 * time.Second}

func NewHandler(manager *auth.Manager, catalog ScriptCatalog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		// Only trust direct TLS or a local reverse proxy, never forwarded headers.
		if r.TLS == nil && !(ip != nil && ip.IsLoopback()) && !strings.HasPrefix(utils.GCFG.Binder, "/") {
			http.Error(w, "TLS required", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		client, err := manager.Verify(strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ServeSession(r.Context(), conn, manager, catalog, client)
	})
}

// ProtocolConnection is a direct websocket or an isolated reverse-link stream.
type ProtocolConnection interface {
	Close() error
	SetReadLimit(int64)
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	ReadMessage() (int, []byte, error)
	WriteJSON(interface{}) error
}

func ServeSession(parent context.Context, conn ProtocolConnection, manager *auth.Manager, catalog ScriptCatalog, client auth.Client) {
	defer conn.Close()
	conn.SetReadLimit(16 << 20)
	write := func(e Event) error {
		e.Protocol = auth.Protocol
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(e)
	}
	fail := func(id, code, message string) {
		write(Event{Type: "finished", TaskID: id, State: "failed", Error: &Failure{code, message}})
	}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return
	}
	cmd, err := decodeCommand(data)
	if err != nil {
		fail("", "invalid_request", err.Error())
		return
	}
	if cmd.Type == "describe" && cmd.Request == nil && cmd.TaskID == "" {
		d := catalog.Describe(client)
		write(Event{Type: "description", Description: &d})
		return
	}
	if cmd.Type != "run" || cmd.Request == nil || !taskIDPattern.MatchString(cmd.TaskID) {
		fail(cmd.TaskID, "invalid_request", "Expected run with a task_id and request")
		return
	}
	req := cmd.Request
	if len(req.Nodes) == 0 {
		fail(cmd.TaskID, "invalid_request", "A task needs at least one node")
		return
	}
	if !vendors.Supported(req.Vendor) {
		fail(cmd.TaskID, "invalid_request", "Unknown vendor")
		return
	}
	if err := client.Allows(req); err != nil {
		fail(cmd.TaskID, "permission_denied", err.Error())
		return
	}
	caps, _ := auth.Required(req)
	if utils.GCFG.NoSpeedFlag && auth.Has(caps, "speed") {
		fail(cmd.TaskID, "permission_denied", "Speed tests disabled")
		return
	}
	// Keep the unexpanded request for authorization: installed scripts are not uploads.
	policyRequest := req.Clone()
	if err := catalog.Resolve(req); err != nil {
		fail(cmd.TaskID, "invalid_request", err.Error())
		return
	}
	req.Configs = *req.Configs.Check()
	release, err := manager.Acquire(client)
	if err != nil {
		fail(cmd.TaskID, "capacity_exceeded", err.Error())
		return
	}
	deadline, deadlineCancel := context.WithTimeout(parent, time.Duration(client.MaxSeconds)*time.Second)
	ctx, cancel := context.WithCancelCause(deadline)
	poll := ConnTaskPoll
	macroTypes := ExtractMacrosFromMatrices(matrices.FindBatchFromEntry(req.Options.Matrices))
	if auth.Has(caps, "speed") {
		poll = SpeedTaskPoll
	}
	// One progress event per node plus one terminal event; no socket writes in workers.
	events := make(chan Event, len(req.Nodes)*32+1)
	internalID := utils.RandomUUID()
	item := (&TestingPollItem{id: internalID, name: cmd.TaskID, ctx: ctx, cancel: cancel, request: req, matrices: req.Options.Matrices, macros: macroTypes,
		onStage: func(index int, stage string, active bool) {
			if req.Configs.StageProgress {
				select {
				case events <- Event{Type: "stage", TaskID: cmd.TaskID, Index: index, Stage: stage, Active: active}:
				case <-ctx.Done():
				}
			}
		},
		authorize: func() error { return manager.StillAllowed(client, policyRequest) },
		onProcess: func(t *TestingPollItem, index int, result interfaces.SlaveEntrySlot) {
			events <- Event{Type: "progress", TaskID: cmd.TaskID, State: "running", Record: &result, Queuing: poll.AwaitingCount()}
		},
		onExit: func(t *TestingPollItem, code taskpoll.TaskPollExitCode) {
			// Workers have really stopped before returning the client's capacity.
			release()
			state := "succeeded"
			var failure *Failure
			cause := context.Cause(ctx)
			if cause != nil {
				state = "cancelled"
				if errors.Is(cause, context.DeadlineExceeded) {
					state = "failed"
					failure = &Failure{"deadline_exceeded", "Task deadline exceeded"}
				} else if errors.As(cause, &failure) {
					if failure.Code != "cancelled" {
						state = "failed"
					}
				}
			} else if code != taskpoll.TPExitSuccess {
				state = "failed"
				failure = &Failure{"execution_failed", "Task execution failed"}
			}
			results := t.results.ForEach()
			sort.Slice(results, func(i, j int) bool { return results[i].Index < results[j].Index })
			events <- Event{Type: "finished", TaskID: cmd.TaskID, State: state, Error: failure, Results: results}
			deadlineCancel()
		},
	}).Init()
	if err = write(Event{Type: "accepted", TaskID: cmd.TaskID, State: "queued"}); err != nil {
		release()
		cancel(context.Canceled)
		deadlineCancel()
		return
	}
	poll.Push(item)
	defer func() { cancel(context.Canceled); poll.Remove(internalID, taskpoll.TPExitInterrupt); deadlineCancel() }()
	conn.SetReadDeadline(time.Time{})
	incoming := make(chan *Command, 1)
	readDone := make(chan struct{})
	defer close(readDone)
	go func() {
		_, data, err := conn.ReadMessage()
		var next *Command
		if err == nil {
			value, decodeErr := decodeCommand(data)
			if decodeErr == nil {
				next = &value
			}
		}
		select {
		case incoming <- next:
		case <-readDone:
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	heartbeats := time.NewTicker(15 * time.Second)
	defer heartbeats.Stop()
	done := ctx.Done()
	for {
		select {
		case next := <-incoming:
			if next == nil {
				return
			}
			if next.Type != "cancel" || next.TaskID != cmd.TaskID || next.Request != nil {
				cancel(&Failure{"invalid_request", "Only cancel is allowed during a task"})
			} else {
				cancel(&Failure{"cancelled", "Cancelled by client"})
			}
			poll.Remove(internalID, taskpoll.TPExitInterrupt)
		case <-done:
			done = nil
			poll.Remove(internalID, taskpoll.TPExitInterrupt)
		case <-ticker.C:
			if ctx.Err() == nil {
				if err := manager.StillAllowed(client, policyRequest); err != nil {
					cancel(&Failure{"permission_revoked", "Client authorization changed"})
					poll.Remove(internalID, taskpoll.TPExitError)
				}
			}
		case <-heartbeats.C:
			if err := write(Event{Type: "heartbeat", TaskID: cmd.TaskID}); err != nil {
				return
			}
		case event := <-events:
			if err := write(event); err != nil {
				return
			}
			if event.Type == "finished" {
				return
			}
		}
	}
}

func InitServer() error {
	catalog, err := LoadScripts(utils.GCFG.ScriptsFile)
	if err != nil {
		return err
	}
	server := http.Server{Handler: NewHandler(auth.NewManager(utils.GCFG.ClientsFile), catalog), ReadHeaderTimeout: 10 * time.Second}
	network := "tcp"
	if strings.HasPrefix(utils.GCFG.Binder, "/") {
		network = "unix"
	}
	listener, err := net.Listen(network, utils.GCFG.Binder)
	if err != nil {
		return err
	}
	if utils.GCFG.TLS {
		cfg, err := preconfigs.MakeTLSServer(utils.GCFG.TLSCertFile, utils.GCFG.TLSKeyFile)
		if err != nil {
			listener.Close()
			return err
		}
		server.TLSConfig = cfg
		err = server.ServeTLS(listener, "", "")
	} else {
		err = server.Serve(listener)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
func CleanUpServer() {
	if strings.HasPrefix(utils.GCFG.Binder, "/") {
		os.Remove(utils.GCFG.Binder)
	}
}

package service

import (
	"github.com/silencoo/speed-probe/auth"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/preconfigs"
	"github.com/silencoo/speed-probe/utils"
	"github.com/silencoo/speed-probe/utils/structs"

	"github.com/silencoo/speed-probe/service/matrices"
	"github.com/silencoo/speed-probe/service/taskpoll"
)

type WsHandler struct {
	Serve func(http.ResponseWriter, *http.Request)
}

func (wh *WsHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if wh.Serve != nil {
		wh.Serve(rw, r)
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func InitServer() {
	manager := auth.NewManager(utils.GCFG.ClientsFile)
	if utils.GCFG.Binder == "" {
		utils.DErrorf("speed-probe Server | Cannot listening the binder, bind=%s", utils.GCFG.Binder)
		os.Exit(1)
	}

	utils.DWarnf("speed-probe Server | Start Listening, bind=%s", utils.GCFG.Binder)

	wsHandler := WsHandler{
		Serve: func(rw http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(rw, r, nil)
			if err != nil {
				utils.DErrorf("speed-probe Test | Socket establishing error, error=%s", err.Error())
				return
			}
			defer conn.Close()
			conn.SetReadLimit(16 << 20)
			var writeMu sync.Mutex
			writeJSON := func(v interface{}) error {
				writeMu.Lock()
				defer writeMu.Unlock()
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				return conn.WriteJSON(v)
			}

			var poll *taskpoll.TaskPollController

			batches := structs.NewAsyncMap[string, bool]()
			cancel := func() {
				if poll != nil {
					for id := range batches.ForEach() {
						poll.Remove(id, taskpoll.TPExitInterrupt)
					}
				}
			}

			defer cancel()
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				request, client, err := authenticate(data, manager)
				if err != nil {
					writeJSON(&interfaces.SlaveResponse{Error: err.Error()})
					return
				}
				sr := *request
				sr.Configs = *sr.Configs.Check()
				for i := range sr.Configs.Scripts {
					if sr.Configs.Scripts[i].TimeoutMillis == 0 {
						sr.Configs.Scripts[i].TimeoutMillis = 10000
					}
					if sr.Configs.Scripts[i].TimeoutMillis > 60000 {
						sr.Configs.Scripts[i].TimeoutMillis = 60000
					}
				}
				caps := append([]string{}, client.Capabilities...)
				if utils.GCFG.NoSpeedFlag {
					filtered := []string{}
					for _, c := range caps {
						if c != "speed" {
							filtered = append(filtered, c)
						}
					}
					caps = filtered
				}
				if len(sr.Nodes) == 0 {
					writeJSON(&interfaces.SlaveResponse{Version: utils.VERSION, Capabilities: caps,
						Result: &interfaces.SlaveTask{Results: []interfaces.SlaveEntrySlot{}}})
					return
				}
				release, err := manager.Acquire(*client)
				if err != nil {
					writeJSON(&interfaces.SlaveResponse{Error: err.Error()})
					return
				}
				authorize := func() error { return manager.StillAllowed(*client, &sr) }
				submitted := false
				defer func() {
					if !submitted {
						release()
					}
				}()

				// find all matrices
				matrices := matrices.FindBatchFromEntry(sr.Options.Matrices)

				// extra macro from the matrices
				macros := ExtractMacrosFromMatrices(matrices)

				// select poll
				if structs.Contains(macros, interfaces.MacroSpeed) {
					if utils.GCFG.NoSpeedFlag {
						writeJSON(&interfaces.SlaveResponse{
							Error: "speedtest is disabled on backend",
						})
						return
					}
					poll = SpeedTaskPoll
				} else {
					poll = ConnTaskPoll
				}
				utils.DLogf("speed-probe Test | Receive Task, name=%s poll=%s", sr.Basics.ID, poll.Name())

				// build testing item
				item := poll.Push((&TestingPollItem{
					id:        utils.RandomUUID(),
					name:      sr.Basics.ID,
					request:   &sr,
					authorize: authorize,
					matrices:  sr.Options.Matrices,
					macros:    macros,
					onProcess: func(self *TestingPollItem, idx int, result interfaces.SlaveEntrySlot) {
						writeJSON(&interfaces.SlaveResponse{
							ID:      self.ID(),
							Version: utils.VERSION,
							Progress: &interfaces.SlaveProgress{
								Record:  result,
								Index:   idx,
								Queuing: poll.AwaitingCount(),
							},
						})
					},
					onExit: func(self *TestingPollItem, exitCode taskpoll.TaskPollExitCode) {
						release()
						batches.Del(self.ID())
						writeJSON(&interfaces.SlaveResponse{
							ID:      self.ID(),
							Version: utils.VERSION,
							Result: &interfaces.SlaveTask{
								Request: sr,
								Results: self.results.ForEach(),
							},
						})
					},
				}).Init())

				submitted = true
				batches.Set(item.ID(), true)
				// One task per connection. Keep reading so a disconnect cancels it.
				conn.ReadMessage()
				return
			}
		},
	}

	server := http.Server{Handler: &wsHandler}

	if strings.HasPrefix(utils.GCFG.Binder, "/") {
		unixListener, err := net.Listen("unix", utils.GCFG.Binder)
		if err != nil {
			utils.DErrorf("speed-probe Launch | Cannot listen on unixsocket %s, error=%s", utils.GCFG.Binder, err.Error())
			os.Exit(1)
		}
		server.Serve(unixListener)
	} else {
		netListener, err := net.Listen("tcp", utils.GCFG.Binder)
		if err != nil {
			utils.DErrorf("speed-probe Launch | Cannot listen on socket %s, error=%s", utils.GCFG.Binder, err.Error())
			os.Exit(1)
		}
		if utils.GCFG.TLS {
			tlsConfig, err := preconfigs.MakeTLSServer(utils.GCFG.TLSCertFile, utils.GCFG.TLSKeyFile)
			if err != nil {
				utils.DErrorf("speed-probe Launch | Cannot configure TLS, error=%s", err.Error())
				os.Exit(1)
			}
			server.TLSConfig = tlsConfig
			server.ServeTLS(netListener, "", "")
		} else {
			server.Serve(netListener)
		}

	}
}

func CleanUpServer() {
	if strings.HasPrefix(utils.GCFG.Binder, "/") {
		os.Remove(utils.GCFG.Binder)
	}
}

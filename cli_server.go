package main

import (
	"flag"
	"os"

	"github.com/silencoo/speed-probe/service"
	"github.com/silencoo/speed-probe/utils"
)

func InitConfigServer() *utils.GlobalConfig {
	gcfg := &utils.GCFG

	sflag := flag.NewFlagSet(cmdName+" server", flag.ExitOnError)
	sflag.StringVar(&gcfg.ClientsFile, "clients", "clients.json", "client credentials file")
	sflag.StringVar(&gcfg.Binder, "bind", "", "bind a socket, can be format like 0.0.0.0:8080 or /tmp/unix_socket")
	sflag.UintVar(&gcfg.ConnTaskTreading, "connthread", 64, "parallel threads when processing normal connectivity tasks")
	sflag.Uint64Var(&gcfg.SpeedLimit, "speedlimit", 0, "speed ratelimit (in Bytes per Second), default with no limits")
	sflag.UintVar(&gcfg.PauseSecond, "pausesecond", 0, "pause such period after each speed job (seconds)")
	sflag.BoolVar(&gcfg.TLS, "tls", false, "enable TLS using -tls-cert and -tls-key")
	sflag.StringVar(&gcfg.TLSCertFile, "tls-cert", "", "path to the PEM-encoded TLS certificate")
	sflag.StringVar(&gcfg.TLSKeyFile, "tls-key", "", "path to the PEM-encoded TLS private key")
	sflag.BoolVar(&gcfg.NoSpeedFlag, "nospeed", false, "decline all speedtest requests")
	sflag.StringVar(&gcfg.MaxmindDB, "mmdb", "", "reroute all geoip query to local mmdbs. for example: test.mmdb,testcity.mmdb")

	parseFlag(sflag)

	return gcfg
}

func RunCliServer() {
	InitConfigServer()
	if err := service.ValidateAuthConfig(); err != nil {
		utils.DErrorf("Authentication configuration: %s", err)
		os.Exit(1)
	}
	utils.DWarnf("speed-probe network testing backend %s", utils.VERSION)

	// load maxmind db
	if utils.LoadMaxMindDB(utils.GCFG.MaxmindDB) != nil {
		os.Exit(1)
	}

	// start task server
	service.StartTaskServer()

	// start api server
	service.CleanUpServer()
	go service.InitServer()

	<-utils.MakeSysChan()

	// clean up
	service.CleanUpServer()
	utils.DLog("shutting down.")
}

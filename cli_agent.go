package main

import (
	"context"
	"flag"
	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/service"
	"github.com/silencoo/speed-probe/utils"
	"os"
	"path/filepath"
)

func RunCliAgent() {
	f := flag.NewFlagSet(cmdName+" agent", flag.ExitOnError)
	path := f.String("config", "agent.json", "agent name, controller address and local policy")
	f.StringVar(&utils.GCFG.ScriptsFile, "scripts", "", "operator-installed scripts")
	f.UintVar(&utils.GCFG.ConnTaskTreading, "connthread", 64, "connectivity concurrency")
	f.Uint64Var(&utils.GCFG.SpeedLimit, "speedlimit", 0, "download speed limit in bytes/s")
	f.UintVar(&utils.GCFG.PauseSecond, "pausesecond", 0, "pause between speed jobs")
	f.BoolVar(&utils.GCFG.NoSpeedFlag, "nospeed", false, "disable speed tests")
	f.StringVar(&utils.GCFG.MaxmindDB, "mmdb", "", "local GeoIP databases")
	parseFlag(f)
	cfg, client, err := service.LoadAgentConfig(*path)
	if err != nil {
		utils.DErrorf("Invalid agent configuration: %s", err)
		os.Exit(1)
	}
	utils.DWarnf("Agent ID: %s; approval fingerprint: %s", cfg.ID, auth.HashToken(cfg.Token)[:16])
	if utils.GCFG.ScriptsFile == "" {
		candidate := filepath.Join(filepath.Dir(*path), "probe-scripts.json")
		if _, err := os.Stat(candidate); !os.IsNotExist(err) {
			utils.GCFG.ScriptsFile = candidate
		}
	}
	catalog, err := service.LoadScripts(utils.GCFG.ScriptsFile)
	if err != nil {
		utils.DErrorf("Invalid script catalog")
		os.Exit(1)
	}
	if utils.LoadMaxMindDB(utils.GCFG.MaxmindDB) != nil {
		os.Exit(1)
	}
	service.StartTaskServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-utils.MakeSysChan():
			cancel()
		case <-ctx.Done():
		}
	}()
	if err = service.RunAgent(ctx, cfg, client, catalog); err != nil {
		os.Exit(1)
	}
}

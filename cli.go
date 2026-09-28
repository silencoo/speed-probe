package main

import (
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/silencoo/speed-probe/utils"
)

var cmdName string = "speed-probe"

type SubCliType string

const (
	SCTMisc       SubCliType = "misc"
	SCTServer     SubCliType = "server"
	SCTScriptTest SubCliType = "script"
)

func RunCli() {
	subCmd := SubCliType("")
	if len(os.Args) >= 2 {
		subCmd = SubCliType(os.Args[1])
	}

	cmdName = path.Base(os.Args[0])
	switch subCmd {
	case "clients":
		RunCliClients()
	case SCTMisc:
		RunCliMisc()
	case SCTServer:
		RunCliServer()
	case SCTScriptTest:
		RunCliScriptTest()
	default:
		RunCliDefault()
	}
}

func RunCliDefault() {
	sflag := flag.NewFlagSet(cmdName, flag.ExitOnError)

	versionOnly := sflag.Bool("version", false, "display version and exit")
	sflag.Parse(os.Args[1:])

	if *versionOnly {
		fmt.Println(utils.VERSION)
		os.Exit(0)
	}

	sflag.Usage()

	fmt.Printf("\n")
	fmt.Printf("Subcommands of %s:\n", cmdName)
	fmt.Printf("  clients\n        manage client credentials (add, list, set, revoke, rotate).\n")
	fmt.Printf("  server\n")
	fmt.Printf("        start the speed-probe backend as a server.\n")
	fmt.Printf("  script\n")
	fmt.Printf("        run a temporary script test to test the correctness of your script.\n")
	fmt.Printf("  misc\n")
	fmt.Printf("        other utility toolkit provided by speed-probe.\n")

	os.Exit(0)
}

func parseFlag(sflag *flag.FlagSet) {
	verboseMode := sflag.Bool("verbose", false, "whether to print out systems log")

	sflag.Parse(os.Args[2:])

	if *verboseMode {
		utils.VerboseLevel = utils.LTLog
	}
}

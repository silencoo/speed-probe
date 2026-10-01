package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/silencoo/speed-probe/auth"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/utils"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
)

type Command struct {
	Protocol int                      `json:"protocol"`
	Type     string                   `json:"type"`
	TaskID   string                   `json:"task_id,omitempty"`
	Request  *interfaces.SlaveRequest `json:"request,omitempty"`
}
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (f *Failure) Error() string { return f.Message }

type Limits struct {
	MaxNodes   int `json:"max_nodes"`
	MaxJobs    int `json:"max_jobs"`
	MaxSeconds int `json:"max_seconds"`
	MaxScripts int `json:"max_scripts"`
}
type Description struct {
	Features        []string     `json:"features"`
	Cores           []CoreInfo   `json:"cores"`
	SoftwareVersion string       `json:"software_version"`
	Supported       []string     `json:"supported"`
	Allowed         []string     `json:"allowed"`
	Limits          Limits       `json:"limits"`
	Scripts         []ScriptInfo `json:"scripts"`
}
type CoreInfo struct {
	ID       string   `json:"id"`
	Version  string   `json:"version"`
	Formats  []string `json:"formats"`
	Features []string `json:"features"`
}

func coreInfo() []CoreInfo {
	cores := []CoreInfo{{"Mihomo", "unknown", []string{"mihomo"}, []string{"quic", "utls"}}, {"SingBox", "unknown", []string{"sing-box"}, []string{"quic", "chain_paths", "exit_verification"}}}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/metacubex/mihomo" {
				cores[0].Version = dep.Version
			}
			if dep.Path == "github.com/sagernet/sing-box" {
				cores[1].Version = dep.Version
			}
		}
		for _, setting := range info.Settings {
			if setting.Key == "-tags" && strings.Contains(setting.Value, "with_utls") {
				cores[1].Features = append(cores[1].Features, "utls")
			}
		}
	}
	return cores
}

type ScriptInfo struct {
	ID   string                `json:"id"`
	Type interfaces.ScriptType `json:"type"`
}
type Event struct {
	Index       int                         `json:"index,omitempty"`
	Stage       string                      `json:"stage,omitempty"`
	Active      bool                        `json:"active"`
	Protocol    int                         `json:"protocol"`
	Type        string                      `json:"type"`
	TaskID      string                      `json:"task_id,omitempty"`
	State       string                      `json:"state,omitempty"`
	Error       *Failure                    `json:"error,omitempty"`
	Description *Description                `json:"description,omitempty"`
	Record      *interfaces.SlaveEntrySlot  `json:"record,omitempty"`
	Results     []interfaces.SlaveEntrySlot `json:"results,omitempty"`
	Queuing     int                         `json:"queuing,omitempty"`
}

func decodeCommand(data []byte) (Command, error) {
	var cmd Command
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&cmd); err != nil {
		return cmd, errors.New("invalid command")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return cmd, errors.New("trailing JSON")
	}
	if cmd.Protocol != auth.Protocol {
		return cmd, errors.New("protocol 3 required")
	}
	return cmd, nil
}

var taskIDPattern = regexp.MustCompile("^[a-zA-Z0-9_-]{1,64}$")

type ScriptCatalog []interfaces.Script

func LoadScripts(path string) (ScriptCatalog, error) {
	scripts := ScriptCatalog{}
	if path == "" {
		return scripts, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, errors.New("script catalog too large")
	}
	if err = json.Unmarshal(data, &scripts); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range scripts {
		if s.ID == "" || seen[s.ID] || s.Content == "" || (s.Type != interfaces.STypeMedia && s.Type != interfaces.STypeIP) {
			return nil, errors.New("invalid or duplicate catalog script")
		}
		seen[s.ID] = true
	}
	return scripts, nil
}
func (c ScriptCatalog) Resolve(req *interfaces.SlaveRequest) error {
	scripts := make([]interfaces.Script, len(req.Configs.Scripts))
	seen := map[string]bool{}
	for i, s := range req.Configs.Scripts {
		if s.ID == "" || seen[s.ID] {
			return errors.New("invalid or duplicate script ID")
		}
		seen[s.ID] = true
		if s.Content == "" {
			found := false
			for _, installed := range c {
				if installed.ID == s.ID {
					s = installed
					found = true
					break
				}
			}
			if !found {
				return errors.New("script is not installed")
			}
		}
		if s.Type != interfaces.STypeMedia && s.Type != interfaces.STypeIP {
			return errors.New("invalid script type")
		}
		if s.TimeoutMillis == 0 {
			s.TimeoutMillis = 10000
		}
		if s.TimeoutMillis > 60000 {
			s.TimeoutMillis = 60000
		}
		scripts[i] = s
	}
	for _, m := range req.Options.Matrices {
		if m.Type == interfaces.MatrixScriptTest {
			found := false
			for _, s := range scripts {
				if s.ID == m.Params && s.Type == interfaces.STypeMedia {
					found = true
				}
			}
			if !found {
				return errors.New("test script reference missing")
			}
		}
	}
	req.Configs.Scripts = scripts
	return nil
}
func (c ScriptCatalog) Describe(client auth.Client) Description {
	allowed := []string{}
	for _, cap := range client.Capabilities {
		if cap != "speed" || !utils.GCFG.NoSpeedFlag {
			allowed = append(allowed, cap)
		}
	}
	list := []ScriptInfo{}
	if auth.Has(allowed, "script") || auth.Has(allowed, "topo") {
		for _, s := range c {
			list = append(list, ScriptInfo{s.ID, s.Type})
		}
	}
	return Description{Features: []string{"chain_paths", "exit_verification", "download_budget", "measurement_details", "upload_speed", "stage_progress", "source_health"}, SoftwareVersion: utils.VERSION, Supported: append([]string{}, auth.Capabilities...), Allowed: allowed, Limits: Limits{client.MaxNodes, client.MaxJobs, client.MaxSeconds, client.MaxScripts}, Scripts: list, Cores: coreInfo()}
}

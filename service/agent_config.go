package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/silencoo/speed-probe/auth"
)

func LoadAgentConfig(path string) (AgentConfig, auth.Client, error) {
	cfg := AgentConfig{Version: 1, Capabilities: []string{"ping", "script", "topo", "speed"}, MaxNodes: 300, MaxJobs: 2, MaxSeconds: 600, MaxScripts: 32}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, auth.Client{}, err
	}
	if len(data) > 65536 {
		return cfg, auth.Client{}, errors.New("agent configuration exceeds 64 KB")
	}
	if json.Unmarshal(data, &cfg) != nil {
		return cfg, auth.Client{}, errors.New("invalid agent JSON")
	}
	u, err := url.Parse(cfg.Controller)
	if err != nil {
		return cfg, auth.Client{}, errors.New("invalid controller URL")
	}
	ip := net.ParseIP(u.Hostname())
	local := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if cfg.Version != 1 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/agent" ||
		(u.Scheme != "wss" && !(u.Scheme == "ws" && local)) {
		return cfg, auth.Client{}, errors.New("invalid controller URL; use WSS except on loopback")
	}
	if len([]rune(cfg.Name)) > 80 || strings.IndexFunc(cfg.Name, unicode.IsControl) >= 0 || (cfg.Name != "" && strings.TrimSpace(cfg.Name) == "") {
		return cfg, auth.Client{}, errors.New("invalid agent name")
	}
	if cfg.ID != "" && !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(cfg.ID) {
		return cfg, auth.Client{}, errors.New("invalid agent ID")
	}
	if cfg.Token == "" {
		if cfg.Name == "" {
			return cfg, auth.Client{}, errors.New("set name and controller for self-registration")
		}
		if err = loadAgentIdentity(path+".identity", &cfg); err != nil {
			return cfg, auth.Client{}, err
		}
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}\.[a-f0-9]{64}$`).MatchString(cfg.Token) || !strings.HasPrefix(cfg.Token, cfg.ID+".") {
		return cfg, auth.Client{}, errors.New("invalid agent identity")
	}
	client := auth.Client{ID: cfg.ID, TokenHash: auth.HashToken(cfg.Token), Capabilities: cfg.Capabilities,
		MaxNodes: cfg.MaxNodes, MaxJobs: cfg.MaxJobs, MaxSeconds: cfg.MaxSeconds, MaxScripts: cfg.MaxScripts}
	return cfg, client, auth.ValidateClient(client)
}

func loadAgentIdentity(path string, cfg *AgentConfig) error {
	type identity struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		random := make([]byte, 40)
		if _, err = rand.Read(random); err != nil {
			return err
		}
		id := cfg.ID
		if id == "" {
			id = "probe-" + hex.EncodeToString(random[:8])
		}
		value := identity{id, id + "." + hex.EncodeToString(random[8:])}
		data, _ = json.Marshal(value)
		// Exclusive creation never replaces an existing or concurrently created identity.
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(openErr, os.ErrExist) {
			return loadAgentIdentity(path, cfg)
		}
		if openErr != nil {
			return errors.New("cannot persist agent identity; make the data directory writable")
		}
		_, err = file.Write(data)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return errors.New("cannot save agent identity")
		}
	} else if err != nil {
		return errors.New("cannot read agent identity")
	}
	var value identity
	if len(data) > 4096 || json.Unmarshal(data, &value) != nil || (cfg.ID != "" && cfg.ID != value.ID) {
		return errors.New("invalid stored agent identity; preserve it and check the configured ID")
	}
	cfg.ID, cfg.Token = value.ID, value.Token
	return nil
}

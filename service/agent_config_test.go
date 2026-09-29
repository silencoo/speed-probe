package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelfRegisteredAgentIdentityPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(`{"name":"Home","controller":"wss://probe.example.com/agent"}`), 0600); err != nil {
		t.Fatal(err)
	}
	first, client, err := LoadAgentConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || first.Token == "" || client.MaxJobs != 2 {
		t.Fatal("missing generated identity or defaults")
	}
	second, _, err := LoadAgentConfig(path)
	if err != nil || first.Token != second.Token || first.ID != second.ID {
		t.Fatal("identity changed on restart", err)
	}
	if err := os.WriteFile(path+".identity", []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = LoadAgentConfig(path); err == nil {
		t.Fatal("must not replace corrupt identity")
	}
	data, _ := os.ReadFile(path + ".identity")
	if string(data) != "broken" {
		t.Fatal("corrupt identity overwritten")
	}
}

func TestAgentRejectsUnsafeRegistrationConfig(t *testing.T) {
	for _, value := range []string{
		`{"name":"Home","controller":"ws://remote.example/agent"}`,
		`{"name":"Home","controller":"wss://user:pass@remote.example/agent"}`,
		`{"name":"Home","controller":"wss://remote.example/agent?token=secret"}`,
		`{"name":"Home\nFake","controller":"wss://remote.example/agent"}`,
	} {
		path := filepath.Join(t.TempDir(), "agent.json")
		os.WriteFile(path, []byte(value), 0600)
		if _, _, err := LoadAgentConfig(path); err == nil {
			t.Fatal("unsafe config accepted")
		}
		if _, err := os.Stat(path + ".identity"); !os.IsNotExist(err) {
			t.Fatal("identity generated for invalid config")
		}
	}
}

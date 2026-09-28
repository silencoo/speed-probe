package main

import (
	"encoding/json"
	"github.com/silencoo/speed-probe/auth"
	"os"
	"path/filepath"
	"testing"
)

func TestIssueRotateAndNoExportedPlaintextInStore(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "clients.json")
	out := filepath.Join(dir, "connection.json")
	if err := runClients([]string{"add", "-file", store, "-id", "test", "-address", "ws://127.0.0.1:8765", "-out", out}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	var connection auth.Connection
	json.Unmarshal(data, &connection)
	manager := auth.NewManager(store)
	if _, err := manager.Verify(connection.Token); err != nil {
		t.Fatal(err)
	}
	if err := runClients([]string{"rotate", "-file", store, "-id", "test", "-address", "ws://127.0.0.1:8765", "-out", out}); err == nil {
		t.Fatal("overwrote credential file")
	}
	if _, err := manager.Verify(connection.Token); err != nil {
		t.Fatal("failed rotation changed token")
	}
	newer := filepath.Join(dir, "rotated.json")
	if err := runClients([]string{"rotate", "-file", store, "-id", "test", "-address", "ws://127.0.0.1:8765", "-out", newer}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Verify(connection.Token); err == nil {
		t.Fatal("old token survived")
	}
}

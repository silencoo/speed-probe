package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/silencoo/speed-probe/auth"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

func RunCliClients() {
	if err := runClients(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func runClients(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: clients <add|list|set|rotate|revoke>; add/rotate require -address and -out")
	}
	action := args[0]
	f := flag.NewFlagSet("clients "+action, flag.ContinueOnError)
	path := f.String("file", "clients.json", "v3 client store")
	id := f.String("id", "", "client ID")
	caps := f.String("capabilities", "ping,script,topo,speed", "allowed capabilities; custom_script requires explicit opt-in")
	nodes := f.Int("max-nodes", 300, "nodes per task")
	jobs := f.Int("max-jobs", 2, "queued and running tasks")
	seconds := f.Int("max-seconds", 600, "task deadline including queue time")
	scripts := f.Int("max-scripts", 32, "scripts per task")
	address := f.String("address", "", "public wss:// URL; loopback ws:// allowed")
	out := f.String("out", "", "new connection file; token is issued once")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if !auth.Has([]string{"add", "list", "set", "rotate", "revoke"}, action) {
		return errors.New("unknown action; export is replaced by add/rotate -address URL -out FILE")
	}
	lock, err := os.OpenFile(*path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("client store is locked")
	}
	lock.Close()
	defer os.Remove(*path + ".lock")
	store, err := auth.Load(*path)
	if err != nil && !(os.IsNotExist(err) && action == "add") {
		return err
	}
	index := -1
	for i, c := range store.Clients {
		if c.ID == *id {
			index = i
		}
	}
	var connection *auth.Connection
	switch action {
	case "list":
		for _, c := range store.Clients {
			fmt.Printf("%s disabled=%v capabilities=%s max_nodes=%d max_jobs=%d max_seconds=%d max_scripts=%d\n", c.ID, c.Disabled, strings.Join(c.Capabilities, ","), c.MaxNodes, c.MaxJobs, c.MaxSeconds, c.MaxScripts)
		}
		return nil
	case "add", "rotate":
		if action == "add" && index >= 0 {
			return errors.New("client already exists")
		}
		if action == "rotate" && (index < 0 || store.Clients[index].Disabled) {
			return errors.New("client not found or disabled")
		}
		u, err := url.Parse(*address)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("invalid WebSocket URL")
		}
		if port := u.Port(); port != "" {
			value, err := strconv.Atoi(port)
			if err != nil || value < 1 || value > 65535 {
				return errors.New("invalid port")
			}
		}
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "wss" && !(u.Scheme == "ws" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))) {
			return errors.New("remote backends require wss://")
		}
		if *out == "" {
			return errors.New("-out is required; the server stores only token hashes")
		}
		token, err := auth.NewToken(*id)
		if err != nil {
			return err
		}
		if action == "add" {
			store.Clients = append(store.Clients, auth.Client{ID: *id, Capabilities: splitCaps(*caps), MaxNodes: *nodes, MaxJobs: *jobs, MaxSeconds: *seconds, MaxScripts: *scripts})
			index = len(store.Clients) - 1
		}
		store.Clients[index].TokenHash = auth.HashToken(token)
		connection = &auth.Connection{Version: auth.Protocol, ID: *id, Name: *id, Address: *address, Token: token}
	case "set", "revoke":
		if index < 0 {
			return errors.New("client not found")
		}
		if action == "revoke" {
			store.Clients[index].Disabled = true
			break
		}
		f.Visit(func(v *flag.Flag) {
			switch v.Name {
			case "capabilities":
				store.Clients[index].Capabilities = splitCaps(*caps)
			case "max-nodes":
				store.Clients[index].MaxNodes = *nodes
			case "max-jobs":
				store.Clients[index].MaxJobs = *jobs
			case "max-seconds":
				store.Clients[index].MaxSeconds = *seconds
			case "max-scripts":
				store.Clients[index].MaxScripts = *scripts
			}
		})
	}
	for _, c := range store.Clients {
		if err := auth.ValidateClient(c); err != nil {
			return err
		}
	}
	if connection != nil {
		data, err := json.MarshalIndent(connection, "", "  ")
		if err != nil {
			return err
		}
		file, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			os.Remove(*out)
			if err != nil {
				return err
			}
			return closeErr
		}
	}
	if err := auth.Save(*path, store); err != nil {
		if connection != nil {
			os.Remove(*out)
		}
		return err
	}
	fmt.Println("Client configuration saved.")
	return nil
}
func splitCaps(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

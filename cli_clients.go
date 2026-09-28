package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/silencoo/speed-probe/auth"
)

func RunCliClients() {
	if err := runClients(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func runClients(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: speed-probe clients <add|list|set|revoke|rotate|export> -id main [-address wss://host:8765] [-out connection.json]")
	}
	action := args[0]
	flags := flag.NewFlagSet("clients "+action, flag.ContinueOnError)
	path := flags.String("file", "clients.json", "client store (reloads on each request)")
	id := flags.String("id", "", "client ID")
	caps := flags.String("capabilities", "ping,script,topo,speed", "allowed test capabilities")
	nodes := flags.Int("max-nodes", 300, "maximum nodes per request")
	jobs := flags.Int("max-jobs", 2, "maximum queued/running jobs")
	address := flags.String("address", "", "public WebSocket URL for export")
	out := flags.String("out", "", "write connection JSON to a new file (0600)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	// Serialize CLI writers; a second process fails instead of losing another edit.
	lock, err := os.OpenFile(*path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("client store is locked; finish the other clients command first")
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
	switch action {
	case "list":
		for _, c := range store.Clients {
			fmt.Printf("%s disabled=%v capabilities=%s max_nodes=%d max_jobs=%d\n", c.ID, c.Disabled, strings.Join(c.Capabilities, ","), c.MaxNodes, c.MaxJobs)
		}
		return nil
	case "add":
		if index >= 0 {
			return errors.New("client already exists")
		}
		secret, err := auth.NewSecret()
		if err != nil {
			return err
		}
		c := auth.Client{ID: *id, Secret: secret, Capabilities: strings.Split(*caps, ","), MaxNodes: *nodes, MaxJobs: *jobs}
		if err := auth.ValidateClient(c); err != nil {
			return err
		}
		store.Clients = append(store.Clients, c)
	case "set":
		if index < 0 {
			return errors.New("client not found")
		}
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "capabilities":
				store.Clients[index].Capabilities = strings.Split(*caps, ",")
			case "max-nodes":
				store.Clients[index].MaxNodes = *nodes
			case "max-jobs":
				store.Clients[index].MaxJobs = *jobs
			}
		})
	case "revoke", "rotate":
		if index < 0 {
			return errors.New("client not found")
		}
		if action == "revoke" {
			store.Clients[index].Disabled = true
		} else {
			secret, err := auth.NewSecret()
			if err != nil {
				return err
			}
			store.Clients[index].Secret = secret
		}
	case "export":
		if index < 0 || store.Clients[index].Disabled {
			return errors.New("client not found or disabled")
		}
		u, err := url.Parse(*address)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("provide a valid WebSocket URL without credentials, query or path")
		}
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "wss" && !(u.Scheme == "ws" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))) {
			return errors.New("use wss:// for remote backends; ws:// is only allowed for loopback")
		}
		c := store.Clients[index]
		data, err := json.MarshalIndent(auth.Connection{Version: 2, ID: c.ID, Name: c.ID, Address: *address, ClientID: c.ID, Secret: c.Secret}, "", "  ")
		if err != nil {
			return err
		}
		if *out == "" {
			return errors.New("-out is required so credentials are not printed to the terminal")
		}
		f, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	default:
		return errors.New("unknown clients action")
	}
	if err := auth.Save(*path, store); err != nil {
		return err
	}
	fmt.Println("Client configuration saved.")
	return nil
}

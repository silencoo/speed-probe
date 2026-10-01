package service

import (
	"context"
	"encoding/json"
	"github.com/silencoo/speed-probe/interfaces"
	"github.com/silencoo/speed-probe/vendors"
	"io"
	"net"
	"strings"
	"time"
)

// This gate completes before any macro, so a mismatched exit cannot transfer a speed payload.
func checkExit(ctx context.Context, proxy interfaces.Vendor, path *interfaces.TestPath) *interfaces.ExitCheck {
	result := &interfaces.ExitCheck{State: "exit_check_failed"}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, _, err := vendors.RequestUnsafe(ctx, proxy, &interfaces.RequestOptions{URL: path.CheckURL, NoRedir: true})
	if err != nil {
		return result
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(b) > 4096 || resp.StatusCode != 200 {
		return result
	}
	text := strings.TrimSpace(string(b))
	var obj struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal(b, &obj) == nil && obj.IP != "" {
		text = obj.IP
	}
	ip := net.ParseIP(text)
	if ip == nil {
		result.State = "exit_check_invalid_ip"
		return result
	}
	result.IP = ip.String()
	result.State = "observed"
	if path.ExpectedIP != "" {
		result.State = "passed"
		if !ip.Equal(net.ParseIP(path.ExpectedIP)) {
			result.State = "unexpected_exit_ip"
		}
	}
	return result
}

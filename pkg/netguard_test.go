package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsPublicIP(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":         true,
		"47.88.1.1":       true,
		"2001:4860::8888": true,
		"127.0.0.1":       false,
		"10.1.2.3":        false,
		"172.16.0.1":      false,
		"192.168.1.1":     false,
		"100.64.0.1":      false, // CGNAT
		"100.100.100.200": false, // Alibaba Cloud metadata service lives in CGNAT space
		"169.254.169.254": false, // link-local metadata endpoint
		"0.0.0.0":         false,
		"224.0.0.1":       false,
		"::1":             false,
		"fe80::1":         false,
		"fc00::1":         false,
	}
	for addr, want := range cases {
		if got := isPublicIP(net.ParseIP(addr)); got != want {
			t.Errorf("isPublicIP(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestPublicOnlyClientRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	_, err := newPublicOnlyHTTPClient(5 * time.Second).Get(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("expected refusal for loopback server, got %v", err)
	}
}

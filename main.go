package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultDomain = "web.oboard.fun"
	defaultDNS    = ":5353"
	defaultHTTP   = ":8080"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError("missing command")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "server":
		return runServer(ctx, args[1:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		port, err := strconv.Atoi(args[0])
		if err != nil || port < 1 || port > 65535 {
			return usageError("expected `server` or a local port")
		}
		return runClient(ctx, port, args[1:])
	}
}

func runServer(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	domain := fs.String("domain", defaultDomain, "authoritative DNS zone and HTTP homepage domain")
	publicIP := fs.String("public-ip", "", "public IPv4 address returned by authoritative DNS")
	dnsAddr := fs.String("dns", defaultDNS, "DNS listen address")
	httpAddr := fs.String("http", defaultHTTP, "HTTP listen address")
	staticDir := fs.String("static", "dist", "built frontend directory")
	forceHTTPS := fs.Bool("homepage-https", true, "redirect homepage HTTP requests to HTTPS when behind a proxy")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *publicIP == "" {
		return usageError("server requires --public-ip")
	}
	ip := net.ParseIP(*publicIP)
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("--public-ip must be an IPv4 address: %q", *publicIP)
	}

	zone, err := NewZone(*domain, ip.To4())
	if err != nil {
		return err
	}

	hub := NewSignalHub()
	httpServer := &http.Server{
		Addr:              *httpAddr,
		Handler:           NewHTTPHandler(zone.Domain, *staticDir, hub, *forceHTTPS),
		ReadHeaderTimeout: 10 * time.Second,
	}

	dnsServers := []*DNSServer{
		NewDNSServer("udp", *dnsAddr, zone),
		NewDNSServer("tcp", *dnsAddr, zone),
	}

	errs := make(chan error, 3)
	for _, srv := range dnsServers {
		srv := srv
		go func() {
			log.Printf("dns %s listening on %s for %s", srv.Network, srv.Addr, zone.Domain)
			errs <- srv.ListenAndServe()
		}()
	}

	go func() {
		log.Printf("http listening on %s for %s", *httpAddr, zone.Domain)
		errs <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		for _, srv := range dnsServers {
			_ = srv.Shutdown()
		}
		return nil
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runClient(ctx context.Context, port int, args []string) error {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	serverURL := fs.String("server", "wss://"+defaultDomain+"/api/client", "client signaling WebSocket URL")
	slug := fs.String("slug", "", "optional session slug; generated when empty")
	stun := fs.String("stun", "stun:stun.l.google.com:19302", "comma-separated STUN/TURN ICE server URLs")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" {
		generated, err := randomSlug()
		if err != nil {
			return err
		}
		*slug = generated
	}
	if err := validateSlug(*slug); err != nil {
		return err
	}

	u, err := url.Parse(*serverURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("slug", *slug)
	u.RawQuery = q.Encode()

	domain := hostWithoutPort(u.Host)
	if after, ok := strings.CutPrefix(domain, "api."); ok {
		domain = after
	}
	log.Printf("session url (http):  http://%s.%s", *slug, domain)
	log.Printf("session url (https): https://%s.%s", *slug, domain)

	client := NewLocalClient(u.String(), port, strings.Split(*stun, ","))
	return client.Run(ctx)
}

func randomSlug() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func validateSlug(slug string) error {
	if len(slug) < 8 || len(slug) > 63 {
		return fmt.Errorf("slug must be 8-63 characters")
	}
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("slug can contain only lowercase letters, numbers, and hyphens")
	}
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return fmt.Errorf("slug cannot start or end with hyphen")
	}
	return nil
}

func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func usageError(message string) error {
	printUsage()
	return errors.New(message)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  turbomesh server --public-ip <ip> [--domain web.oboard.fun] [--dns :53] [--http :8080]
  turbomesh <port> [--server wss://web.oboard.fun/api/client] [--slug <slug>]

The server is authoritative for web.oboard.fun and relays WebRTC signaling only.
The client proxies localhost:<port> over WebRTC DataChannels.`)
}

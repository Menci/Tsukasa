package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"

	"tailscale.com/net/socks5"
)

// Copied from https://github.com/tailscale/tailscale/blob/a2c42d3cd4e914b8ac879ac0a21c284ecaf143fc/cmd/tailscaled/proxy.go#L21
//
// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause
//
// httpProxyHandler returns an HTTP proxy http.Handler using the
// provided backend dialer.
func httpProxyHandler(dialer func(ctx context.Context, netw, addr string) (net.Conn, error)) http.Handler {
	rp := &httputil.ReverseProxy{
		Director: func(r *http.Request) {}, // no change
		Transport: &http.Transport{
			DialContext: dialer,
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" {
			backURL := r.RequestURI
			if strings.HasPrefix(backURL, "/") || backURL == "*" {
				http.Error(w, "bogus RequestURI; must be absolute URL or CONNECT", 400)
				return
			}
			rp.ServeHTTP(w, r)
			return
		}

		// CONNECT support:

		dst := r.RequestURI
		c, err := dialer(r.Context(), "tcp", dst)
		if err != nil {
			w.Header().Set("Tailscale-Connect-Error", err.Error())
			http.Error(w, err.Error(), 500)
			return
		}
		defer c.Close()

		cc, ccbuf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer cc.Close()

		io.WriteString(cc, "HTTP/1.1 200 OK\r\n\r\n")

		var clientSrc io.Reader = ccbuf
		if ccbuf.Reader.Buffered() == 0 {
			// In the common case (with no
			// buffered data), read directly from
			// the underlying client connection to
			// save some memory, letting the
			// bufio.Reader/Writer get GC'ed.
			clientSrc = cc
		}

		errc := make(chan error, 1)
		go func() {
			_, err := io.Copy(cc, c)
			errc <- err
		}()
		go func() {
			_, err := io.Copy(c, clientSrc)
			errc <- err
		}()
		<-errc
	})
}

type ProxyType string

const (
	Socks5 ProxyType = "SOCKS5"
	HTTP   ProxyType = "HTTP"
)

type Dialer func(ctx context.Context, network, address string) (net.Conn, error)

func StartProxy(logger *Logger, address string, dialer Dialer, proxyType ProxyType) {
	var listener net.Listener
	var err error
	var cleanup func()

	if strings.HasPrefix(address, "unix:") {
		filename := address[5:]
		listener, err = net.Listen("unix", filename)
		cleanup = func() {
			listener.Close()
			os.Remove(filename)
		}
	} else {
		listener, err = net.Listen("tcp", address)
		cleanup = func() {
			listener.Close()
		}
	}

	if err != nil {
		logger.Fatalf("failed to start %s proxy on %s: %v", proxyType, address, err)
	}

	var serve func(listener net.Listener) error
	switch proxyType {
	case Socks5:
		ss := &socks5.Server{
			Logf:   logger.Verbosef,
			Dialer: dialer,
		}
		serve = ss.Serve
	case HTTP:
		hs := &http.Server{
			Handler: httpProxyHandler(dialer),
		}
		serve = hs.Serve
	default:
		logger.Fatalf("unknown proxy type: %s", proxyType)
	}

	go func() {
		err := serve(listener)
		cleanup()
		if err != nil {
			logger.Fatalf("failed to serve %s proxy on %s: %v", proxyType, address, err)
		}
	}()
	logger.Infof("started %s proxy on %s", proxyType, address)
}

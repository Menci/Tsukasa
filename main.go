package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"tailscale.com/tsnet"
)

type Logf func(format string, args ...any)

func main() {
	logger := CreateLogger("tsukasa", Info)

	config, err := GetConfig()
	if err != nil {
		logger.Fatalf("invalid config: %v", err)
	}

	if err := config.ValidateServices(); err != nil {
		logger.Fatalf("invalid service config: %v", err)
	}

	tsnet := new(tsnet.Server)
	tsnet.Hostname = config.Tailscale.Hostname
	tsnet.AuthKey = config.Tailscale.AuthKey
	tsnet.Ephemeral = config.Tailscale.Ephemeral
	tsnet.Dir = config.Tailscale.StateDir
	tsnet.UserLogf = log.New(os.Stderr, "[tsnet] ", log.LstdFlags).Printf
	if config.Tailscale.Verbose {
		tsnet.Logf = tsnet.UserLogf
	} else {
		tsnet.Logf = func(format string, args ...any) {}
	}

	shutdownCh := make(chan struct{})
	shutdownWg := &sync.WaitGroup{}
	serviceContext := &ServiceContext{
		TsNet:      tsnet,
		ShutdownCh: shutdownCh,
		ShutdownWg: shutdownWg,
	}

	usingTailscale := false
	var services []*Service
	for name, serviceConfig := range config.Services {
		service, err := CreateService(serviceContext, name, serviceConfig)
		if err != nil {
			logger.Fatalf("failed to create service %q: %v", name, err)
		}
		if service.ListenType == AddressTailscaleTCP || service.ConnectType == AddressTailscaleTCP {
			usingTailscale = true
		}
		services = append(services, service)
	}

	if usingTailscale {
		if err := config.ValidateTailscaleConfig(); err != nil {
			logger.Fatalf("Tailscale used but got invalid Tailscale config: %v", err)
		}
		if config.Tailscale.AuthKey == "" {
			logger.Infof("Tailscale authkey not provided, will try interactive login")
		}
		if err := tsnet.Start(); err != nil {
			logger.Fatalf("failed to start Tailscale: %v", err)
		}
		defer tsnet.Close()
	}

	// Start services.
	if len(services) == 0 {
		logger.Fatalf("no services defined. run %s -h for help", os.Args[0])
	}
	for _, service := range services {
		go service.Start()
	}

	// Wait for signal to shutdown.
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
	close(shutdownCh)
	shutdownWg.Wait()
}

package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"sync"

	"tailscale.com/tsnet"
)

type AddressType int

const (
	AddressTCP AddressType = iota
	AddressUNIXSocket
	AddressTailscaleTCP
)

type ServiceContext struct {
	TsNet      *tsnet.Server
	ShutdownCh chan struct{}
	ShutdownWg *sync.WaitGroup
}

type Service struct {
	ServiceContext *ServiceContext
	Config         *ServiceConfig

	Name                 string
	ListenType           AddressType
	ListenAddress        string
	ListenPort           int16
	ConnectType          AddressType
	ConnectAddress       string
	ConnectPort          int16
	ConnectProxyProtocol bool
	LogLevel             LogLevel
}

func parsePort(portString string) (int16, error) {
	if portString == "" {
		return 0, fmt.Errorf("empty port")
	} else if port, err := strconv.ParseInt(portString, 10, 16); err != nil {
		return 0, fmt.Errorf("invalid port")
	} else {
		return int16(port), nil
	}
}

type urlType string

const (
	urlTypeListen  urlType = "listen"
	urlTypeConnect urlType = "connect"
)

func parseUrl(urlType urlType, urlString string) (addressType AddressType, address string, port int16, e error) {
	if url, err := url.Parse(urlString); err != nil {
		e = fmt.Errorf("failed to parse %s URL: %v", urlType, err)
	} else {
		switch url.Scheme {
		case "tcp":
			if port, err = parsePort(url.Port()); err != nil {
				e = fmt.Errorf("failed to parse %s port: %v", urlType, err)
			} else {
				addressType = AddressTCP
				address = url.Hostname()
			}
		case "unix":
			addressType = AddressUNIXSocket
			address = url.Path
		case "tailscale":
			// Allowed ListenAddress for Tailscale is "::" or "0.0.0.0"
			if urlType == urlTypeListen && (url.Hostname() != "::" && url.Hostname() != "0.0.0.0") {
				e = fmt.Errorf("invalid Tailscale %s address: %s (only \"::\" and \"0.0.0.0\" allowed)", urlType, url.Hostname())
			} else if port, err = parsePort(url.Port()); err != nil {
				e = fmt.Errorf("failed to parse %s port: %v", urlType, err)
			} else {
				addressType = AddressTailscaleTCP
				address = url.Hostname()
			}
		default:
			e = fmt.Errorf("unsupported %s URL scheme: %s", urlType, url.Scheme)
		}
	}
	return
}

func CreateService(serviceContext *ServiceContext, name string, config *ServiceConfig) (service *Service, err error) {
	service = &Service{
		ServiceContext:       serviceContext,
		Config:               config,
		Name:                 name,
		LogLevel:             config.LogLevel,
		ConnectProxyProtocol: config.ProxyProtocol,
	}
	if service.ListenType, service.ListenAddress, service.ListenPort, err = parseUrl(urlTypeListen, config.Listen); err != nil {
		return nil, err
	}
	if service.ConnectType, service.ConnectAddress, service.ConnectPort, err = parseUrl(urlTypeConnect, config.Connect); err != nil {
		return nil, err
	}
	return
}

func (s *Service) Listen() (listener net.Listener, cleanup func(), err error) {
	switch s.ListenType {
	case AddressTCP:
		listener, err = net.Listen("tcp", s.ListenAddress+":"+strconv.Itoa(int(s.ListenPort)))
		cleanup = func() {
			listener.Close()
		}
	case AddressUNIXSocket:
		listener, err = net.Listen("unix", s.ListenAddress)
		cleanup = func() {
			listener.Close()
			os.Remove(s.ListenAddress)
		}
	case AddressTailscaleTCP:
		listener, err = s.ServiceContext.TsNet.Listen("tcp", ":"+strconv.Itoa(int(s.ListenPort)))
		cleanup = func() {
			listener.Close()
		}
	default:
		return nil, nil, fmt.Errorf("invalid listen address type: %v", s.ListenType)
	}
	return
}

func (s *Service) CreateConnector() (func() (net.Conn, error), error) {
	switch s.ConnectType {
	case AddressTCP:
		return func() (net.Conn, error) {
			return net.Dial("tcp", s.ConnectAddress+":"+strconv.Itoa(int(s.ConnectPort)))
		}, nil
	case AddressUNIXSocket:
		return func() (net.Conn, error) {
			return net.Dial("unix", s.ConnectAddress)
		}, nil
	case AddressTailscaleTCP:
		return func() (net.Conn, error) {
			return s.ServiceContext.TsNet.Dial(context.Background(), "tcp", s.ConnectAddress+":"+strconv.Itoa(int(s.ConnectPort)))
		}, nil
	default:
		return nil, fmt.Errorf("invalid connect address type: %v", s.ConnectType)
	}
}

func (s *Service) Start() {
	logger := CreateLogger("services/"+s.Name, s.LogLevel)

	listener, cleanup, err := s.Listen()
	if err != nil {
		logger.Errorf("failed to create listener: %v", err)
		return
	}

	connector, err := s.CreateConnector()
	if err != nil {
		logger.Errorf("failed to create connector: %v", err)
		return
	}

	logger.Infof("listening on %s", s.Config.Listen)
	s.ServiceContext.ShutdownWg.Add(1)

	connCh := make(chan net.Conn)
	go func() {
		for {
			conn, err := listener.Accept()
			select {
			case <-s.ServiceContext.ShutdownCh:
				return
			default:
				if err != nil {
					logger.Errorf("failed to accept connection: %v", err)
					continue
				}
				logger.Verbosef("accepted connection from %v", conn.RemoteAddr())
				connCh <- conn
			}
		}
	}()

	for {
		select {
		case <-s.ServiceContext.ShutdownCh:
			cleanup()
			s.ServiceContext.ShutdownWg.Done()
			return
		case conn := <-connCh:
			go func() {
				targetConn, err := connector()
				if err != nil {
					logger.Errorf("failed to connect to target: %v", err)
					conn.Close()
					return
				}
				logger.Verbosef("connected to target %v", targetConn.RemoteAddr())

				if s.Config.ProxyProtocol {
					varsion, remoteIp, remotePort := tryExtractAddr(conn.RemoteAddr())
					_, localIp, localPort := tryExtractAddr(conn.LocalAddr())
					header := fmt.Sprintf("PROXY TCP%d %s %s %d %d\r\n", varsion, remoteIp, localIp, remotePort, localPort)
					logger.Verbosef("writing PROXY Protocol header: %v", header)
					if _, err := targetConn.Write([]byte(header)); err != nil {
						logger.Errorf("failed to write PROXY Protocol header: %v", err)
						targetConn.Close()
						conn.Close()
						return
					}
				}

				PipeAndClose(conn, targetConn, logger)
			}()
		}
	}
}

func tryExtractAddr(addr net.Addr) (version int, ip string, port int) {
	switch addr := addr.(type) {
	case *net.TCPAddr:
		if addr.IP.To4() == nil {
			version = 6
		} else {
			version = 4
		}
		ip = addr.IP.String()
		port = addr.Port
	default:
		version = 4
		ip = "0.0.0.0"
		port = 0
	}
	return
}

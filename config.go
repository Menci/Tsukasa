package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"
)

type TailscaleConfig struct {
	Hostname  string `yaml:"hostname,omitempty"`
	AuthKey   string `yaml:"authKey"`
	Ephemeral bool   `yaml:"ephemeral,omitempty"`
	StateDir  string `yaml:"stateDir"`
	Verbose   bool   `yaml:"verbose,omitempty"`
}

type ServiceConfig struct {
	Listen        string `yaml:"listen"`
	Connect       string `yaml:"connect"`
	logLevel      string `yaml:"logLevel,omitempty"`
	ProxyProtocol bool   `yaml:"proxyProtocol,omitempty"`

	LogLevel LogLevel
}

func parseLogLevel(s string) (LogLevel, error) {
	switch s {
	case "error":
		return Error, nil
	case "info":
		return Info, nil
	case "verbose":
		return Verbose, nil
	case "":
		return Info, nil
	default:
		return 0, fmt.Errorf("unknown log level: %s", s)
	}
}

type Config struct {
	Tailscale TailscaleConfig           `yaml:"tailscale"`
	Services  map[string]*ServiceConfig `yaml:"services"`
}

// A private struct to hold the command-line flags
type arguments struct {
	conf        *string
	tsHostname  *string
	tsAuthKey   *string
	tsEphemeral *bool
	tsStateDir  *string
	tsVerbose   *bool

	services []string
}

func parseArguments() *arguments {
	flags := &arguments{
		conf:        flag.String("conf", "", "YAML Configuration file"),
		tsHostname:  flag.String("ts-hostname", "", "Tailscale hostname"),
		tsAuthKey:   flag.String("ts-authkey", "", "Tailscale authentication key (default to $TS_AUTHKEY)"),
		tsEphemeral: flag.Bool("ts-ephemeral", false, "Set the Tailscale host to ephemeral"),
		tsStateDir:  flag.String("ts-state-dir", "", "Tailscale state directory"),
		tsVerbose:   flag.Bool("ts-verbose", false, "Print Tailscale logs"),
	}
	flag.Usage = func() {
		f := flag.CommandLine.Output()
		fmt.Fprintf(f, "Usage: %s [options] service1 service2 ...\n", os.Args[0])
		fmt.Fprint(f, "\nTsukasa - A flexible port forwarder among TCP, UNIX Socket and Tailscale TCP ports.\n\n")
		flag.PrintDefaults()
		fmt.Fprintf(f, "\nExample: %s \\\n", os.Args[0])
		fmt.Fprintln(f, "    --ts-hostname Tsukasa \\")
		fmt.Fprintln(f, "    --ts-authkey \"$TS_AUTHKEY\" \\")
		fmt.Fprintln(f, "    --ts-ephemeral false \\")
		fmt.Fprintln(f, "    --ts-state-dir /var/lib/tailscale \\")
		fmt.Fprintln(f, "    --ts-verbose true \\")
		fmt.Fprintln(f, "    nginx,listen=tailscale://0.0.0.0:80,connect=tcp://127.0.0.1:8080,log-level=info,proxy-protocol \\")
		fmt.Fprintln(f, "    myapp,listen=unix:/var/run/myapp.sock,connect=tailscale://app-hosted-in-tailnet:8080")

	}
	flag.Parse()
	flags.services = flag.Args()
	return flags
}

var nameRegexp = regexp.MustCompile(`^[$a-zA-Z0-9_-]+$`)

func parseService(s string) (name string, service *ServiceConfig, err error) {
	// Examples:
	// 		 nginx,listen=tailscale://0.0.0.0:80,connect=tcp://127.0.0.1:8080,log-level=info,proxy-protocol
	// 		 myapp,listen=unix:/var/run/myapp.sock,connect=tailscale://app-hosted-in-tailnet:8080

	// Split the string by commas
	parts := strings.Split(s, ",")

	// The first part is the service name
	name = parts[0]
	parts = parts[1:]

	if !nameRegexp.MatchString(name) {
		return "", nil, fmt.Errorf("invalid service name: %s", name)
	}

	// The rest of the parts are key-value pairs
	service = &ServiceConfig{}
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)

		key := kv[0]
		var value *string
		if len(kv) == 2 {
			value = &kv[1]
		}

		switch key {
		case "listen":
			if value == nil {
				return "", nil, fmt.Errorf("required value for option `listen`")
			}
			service.Listen = *value
		case "connect":
			if value == nil {
				return "", nil, fmt.Errorf("required value for option `connect`")
			}
			service.Connect = *value
		case "log-level":
			if value == nil {
				return "", nil, fmt.Errorf("required value for option `log-level`")
			}
			service.logLevel = *value
		case "proxy-protocol":
			if value != nil {
				return "", nil, fmt.Errorf("no value expected for option `proxy-protocol`")
			}
			service.ProxyProtocol = true
		default:
			return "", nil, fmt.Errorf("unknown service argument: %s", key)
		}
	}

	return name, service, nil
}

func mergeConfig(c *Config, a *arguments) error {
	if a.tsHostname != nil && *a.tsHostname != "" {
		c.Tailscale.Hostname = *a.tsHostname
	}

	if a.tsAuthKey != nil && *a.tsAuthKey != "" {
		c.Tailscale.AuthKey = *a.tsAuthKey
	}

	if a.tsEphemeral != nil {
		c.Tailscale.Ephemeral = *a.tsEphemeral
	}

	if a.tsStateDir != nil && *a.tsStateDir != "" {
		c.Tailscale.StateDir = *a.tsStateDir
	}

	if a.tsVerbose != nil {
		c.Tailscale.Verbose = *a.tsVerbose
	}

	for _, s := range a.services {
		name, service, err := parseService(s)
		if err != nil {
			return err
		}
		c.Services[name] = service
	}

	return nil
}

func (c *Config) ValidateTailscaleConfig() error {
	if c.Tailscale.Hostname == "" {
		return fmt.Errorf("missing Tailscale hostname")
	}

	if c.Tailscale.StateDir == "" {
		return fmt.Errorf("missing Tailscale state directory")
	}

	return nil
}

func (c *Config) ValidateServices() error {
	for name, service := range c.Services {
		if service.Listen == "" {
			return fmt.Errorf("missing listen address for service %s", name)
		}

		if service.Connect == "" {
			return fmt.Errorf("missing connect address for service %s", name)
		}
	}

	return nil
}

func GetConfig() (*Config, error) {
	a := parseArguments()

	c := &Config{
		Tailscale: TailscaleConfig{},
		Services:  make(map[string]*ServiceConfig),
	}
	if *a.conf != "" {
		f, err := os.Open(*a.conf)
		if err != nil {
			return nil, err
		}

		err = yaml.NewDecoder(f).Decode(&c)
		if err != nil {
			return nil, err
		}
	}

	if err := mergeConfig(c, a); err != nil {
		return nil, err
	}

	if c.Tailscale.AuthKey == "" {
		c.Tailscale.AuthKey = os.Getenv("TS_AUTHKEY")
	}

	for name, service := range c.Services {
		var err error
		if service.LogLevel, err = parseLogLevel(service.logLevel); err != nil {
			return nil, fmt.Errorf("invalid log level for service %s: %v", name, err)
		}
	}

	return c, nil
}

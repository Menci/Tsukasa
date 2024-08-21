# Tsusaka

Tsusaka is a flexible port forwarder among:

* TCP Ports
* UNIX Sockets
* Tailscale TCP Ports (without Tailscale daemon or TUN/TAP permission! This is made possible with [tsnet](https://tailscale.com/kb/1244/tsnet))
  * If you don't use Tailscale features, it won't initialize Tailscale components and just behaves like a local port forwarder.

It also supports passing the client IP with PROXY protocol (for listening on TCP or Tailscale TCP).

> The name Tsusaka comes from the character **Tenma Tsukasa** from the music visual novel game Project SEKAI. He is a member of the musical show unit "Wonderlands x Showtime". Tsukasa has bucketloads of confidence and loves to be the center of attention. A theater show he saw as a kid impressed him so much that he made it his ultimate goal to become the greatest star in the world.

# Development

Simply build the program with `go build` or build the Docker image with `docker build`.

# Usage

Use with command-line configuration:

```bash
./tsusaka --ts-hostname Tsusaka \
          --ts-authkey "$TS_AUTHKEY" \
          --ts-ephemeral false \
          --ts-state-dir /var/lib/tailscale \
          --ts-verbose true \
          nginx,listen=tailscale://0.0.0.0:80,connect=tcp://127.0.0.1:8080,log-level=info,proxy-protocol \
          myapp,listen=unix:/var/run/myapp.sock,connect=tailscale://app-hosted-in-tailnet:8080
```

Or use with configuration file:

```yaml
# Tailscale configuration is not required and Tailsccale will not be loaded if no services with Tailscale defined.
tailscale:
  hostname: Tsusaka
  # `null` to Use `TS_AUTHKEY` from environment or interactive login.
  authKey: null
  ephemeral: false
  stateDir: /var/lib/tailscale
  verbose: true
services:
  nginx:
    listen: tailscale://0.0.0.0:80 # Only "0.0.0.0" and "::" allowed in Tailscale listener.
    connect: tcp://127.0.0.1:8080
    logLevel: info # "error" / "info" / "verbose". By default "info".
    proxyProtocol: true # Listening on UNIX socket doesn't support PROXY protocol.
  myapp:
    listen: unix:/var/run/myapp.sock
    connect: tailscale://app-hosted-in-tailnet:8080
```

Configuration file could be specified with command-line configuration options at the same time.

```bash
./tsusaka --conf tsusaka.yaml
```

You can also completely omit Tailscale-related configuration and use Tsukasa as a simple port forward between TCP port and UNIX socket.

# Docker

To use Tsukasa with Docker, it's recommended to start Tsusaka in the host network mode to ensure Tailscale's UDP hole punching to work (Docker's MASQUERADE routing is nearly blocking NAT traversal).

```bash
docker run \
  --network=host \
  -e TS_AUTHKEY="$TS_AUTHKEY" \
  -v ./tailscale-state:/var/lib/tailscale \
  ghcr.io/menci/tsusaka \
  --ts-hostname Tsusaka \
  --ts-state-dir /var/lib/tailscale \
  myapp,listen=tcp://0.0.0.0:80,connect=tailscale://app-hosted-in-tailnet:8080
```

If you want to expose something in a container to your Tailnet, use UNIX socket and a shared volume. Here is an example with [Docker Compose](https://docs.docker.com/compose/). Note that if your application doesn't support listening on a UNIX socket, you can also start another instance of Tsukasa to work as a simple port forwarder from/to UNIX socket and TCP port in the virtual network.

```yaml
services:
  initialize:
    image: busybox
    command: |
      # The initialize container empties the shared-sockets directory each time.
      rm -rf /socket/*
    volumes:
      - shared-sockets:/socket
  tsukasa:
    image: ghcr.io/menci/tsukasa
    network_mode: host
    depends_on:
      initialize:
        condition: service_completed_successfully
    volumes:
      - tailscale-state:/var/lib/tailscale
      - shared-sockets:/socket
    environment:
      TS_AUTHKEY: ${TAILSCALE_AUTHKEY}
    command:
      - app,listen=tailscale://0.0.0.0:80,connect=unix:/socket/app.sock
  app:
    image: # Here comes your app, which listens on /socket/app.sock
    depends_on:
      initialize:
        condition: service_completed_successfully
    volumes:
      - shared-sockets:/socket
    command: my_app --listen /socket/app.sock
```

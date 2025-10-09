# Karoo Stratum Proxy

> Author: Carlos Rabelo - contato@carlosrabelo.com.br

Karoo started as a weekend experiment: a lightweight Stratum proxy so a rack of Nerdminers could share a single upstream connection. The idea quickly grew into a general-purpose, multi-protocol Stratum front-end that keeps upstream pools happy while CPU, GPU, or embedded rigs hammer away behind it.

The codebase follows Go best practices with a clean, modular architecture for maintainability and extensibility.

## Features

- **Upstream on demand** – automatically dials the configured pool only when miners are connected and backs off with jittered retries on failures.
- **Protocol aware fan-out** – normalises `mining.subscribe`, `mining.authorize`, and `mining.submit` flows while preserving client IDs and minimizing extranonce collisions.
- **Share accounting & reporting** – detailed logs per worker (latency, spacing between accepted shares) plus periodic aggregate reports and HTTP `/status` / `/healthz` endpoints.
- **VarDiff scaffold** – optional, per-worker difficulty adjustments with room to grow into a full moving-average controller.
- **Flexible transport** – TCP today, with plumbing designed to extend to TLS or additional wire protocols.

## Getting Started

```bash
make build        # compile to bin/karoo
make run          # build and run with ./config.json
make all          # clean, format, vet, and build
```

The default configuration listens on `:3334` for Stratum clients and connects to the upstream pool defined in `config.json`. HTTP status endpoints are exposed at `:8080` by default.

### Configuration Highlights

- `proxy.listen` – downstream address the miners connect to.
- `upstream.{host,port,user,pass}` – upstream Stratum pool credentials.
- `proxy.client_idle_ms` – disconnect idle miners after the specified timeout.
- `compat.strict_broadcast` – when `false`, forwards unfamiliar `mining.*` messages unchanged.
- `vardiff.enabled` – toggle simple per-worker VarDiff adjustments.

See `config.example.json` for a full configuration reference.

## Architecture

The codebase is organized following Go standards:

- `cmd/karoo/` - Main application entry point
- `internal/` - Private application modules:
  - `client/` - Client connection management
  - `config/` - Configuration loading and validation
  - `metrics/` - Statistics and monitoring
  - `protocol/` - Stratum protocol definitions
  - `proxy/` - Core proxy logic and orchestration
  - `upstream/` - Upstream pool connection management
  - `utils/` - Shared utilities and helpers

## Development Workflow

- Build with `make build` (outputs `bin/karoo`)
- Run quality checks with `make fmt vet lint`
- Test with `go test ./...` (tests to be added)
- Full build pipeline with `make all`

## Roadmap

- Expand the VarDiff loop into a moving average controller with bucketed share statistics.
- Add downstream protocol adapters (e.g., WebSockets) and upstream failover lists.
- Ship structured metrics (Prometheus/OpenTelemetry) to complement the existing logs.

## License

Karoo is released under the GNU General Public License, version 2. See [LICENSE](LICENSE) for the full text.

## Donations

If Karoo is useful to you, consider supporting development:

- **BTC**: `bc1qw2raw7urfuu2032uyyx9k5pryan5gu6gmz6exm`
- **ETH**: `0xdb4d2517C81bE4FE110E223376dD9B23ca3C762E`
- **TRX**: `TTznF3FeDCqLmL5gx8GingeahUyLsJJ68A`

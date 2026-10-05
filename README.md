# SHORTLINK-API

A production-minded URL shortener written in Go. It supports generated short
codes, custom aliases, optional expiration, redirect visit counts, health
checks, structured logs, SQLite persistence, and graceful shutdown.

## Requirements

- Go 1.25 or newer

## Run

```powershell
go mod download
go run ./cmd/server
```

The API starts at `http://localhost:8080` by default.

Create a short link:

```powershell
Invoke-RestMethod `
  -Method Post `
  -Uri http://localhost:8080/api/v1/urls `
  -ContentType application/json `
  -Body '{"url":"https://go.dev/doc/"}'
```

Then open the returned `short_url` in a browser.

## Configuration

Configuration is read from environment variables. See `.env.example` for all
available settings. The server creates the database directory and applies
embedded SQL migrations during startup.

## Project Structure

```text
cmd/server             Application entry point
internal/config        Environment configuration
internal/database      Database setup and migration runner
internal/handler       HTTP routes and request handling
internal/middleware    Logging, recovery, timeouts, request IDs
internal/model         Domain models
internal/repository    Persistence contract and SQLite implementation
internal/service       Validation and business logic
internal/shortener     Cryptographically random code generator
pkg/response           Reusable JSON response helpers
migrations             Embedded SQL schema
scripts                Local development commands
docs                   API documentation
tests/integration      End-to-end HTTP tests
```

## Verify

```powershell
go test ./...
go vet ./...
go build -o bin/url-shortener.exe ./cmd/server
```

## Deploy to AWS EC2

The repository includes an Amazon Linux 2023 deployment using Nginx and
`systemd`. Follow [docs/aws-ec2.md](docs/aws-ec2.md) for the EC2 security
group, SSH, installation, health-check, and update instructions.

## Scaling Path

The HTTP, service, and persistence layers are separated intentionally. For a
larger deployment, implement `repository.LinkRepository` with PostgreSQL,
place Redis in front of redirect lookups, and run multiple stateless server
instances behind a load balancer. SQLite is appropriate for local use and
small single-node deployments.

testing

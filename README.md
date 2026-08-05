# Sentinel RMM Platform

**Unified Endpoint Management, Everywhere**

Sentinel is a modern, cloud-hosted Remote Monitoring and Management (RMM) platform designed to monitor and manage endpoints across Windows, Linux, and macOS. Built with a focus on performance, scalability, and ease of use.

> **⚠ Authorized Deployment Only.** Sentinel is a remote monitoring and
> management platform. Deploying it on systems you do not own, administer, or
> have explicit written authorization to manage is illegal in most
> jurisdictions. You are solely responsible for ensuring any deployment
> complies with applicable law and your organization's policies. See
> [SECURITY.md](SECURITY.md) to report vulnerabilities.
>
> **No Warranty.** This software is provided "AS IS" under the terms of the
> Apache License 2.0 (see [LICENSE](LICENSE)). The authors make no
> representations about suitability for any particular purpose, including but
> not limited to regulatory compliance, endpoint-protection guarantees, or
> data-integrity assurances.

## Features

- **Real-time Device Monitoring** - CPU, memory, disk, and network metrics
- **Remote Command Execution** - Run commands and scripts on managed devices
- **Alerting System** - Configurable threshold-based alerts with multiple notification channels
- **Script Library** - Store and execute PowerShell, Bash, and Python scripts
- **WebSocket Communication** - Low-latency, bi-directional agent communication
- **Multi-platform Agent** - Cross-platform Go agent for Windows, Linux, and macOS
- **Modern Web Dashboard** - React-based responsive UI

## Architecture

Web traffic for the Sentinel frontend and backend is routed by the shared
`infra-traefik` edge proxy (in the `~/infra/` stack on NEXUS) via the external
`edge` docker network. Sentinel itself runs a dedicated `sentinel-agent-gateway`
Traefik instance that handles only agent-facing protocols on ports 8443 (mTLS)
and 4444 (gRPC dataplane).

```
┌──────────────────────────────────────────────────────────────┐
│                infra-traefik  (~/infra/ stack)                │
│              :80 / :443  — edge web routing                   │
└──────────────┬────────────────────────────────┬───────────────┘
               │ edge network                   │
               ▼                                ▼
┌─────────────────────────────────────────────────────────────┐
│                     Sentinel Platform                        │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────┐                              ┌────────────┐│
│  │   React     │                              │   Go       ││
│  │   Frontend  │                              │   Backend  ││
│  └─────────────┘                              └────────────┘│
│                                                     │        │
│                              ┌──────────────────────┤        │
│                              │                      │        │
│                        ┌─────▼─────┐          ┌─────▼─────┐  │
│                        │ PostgreSQL│          │   Redis   │  │
│                        │  Database │          │   Cache   │  │
│                        └───────────┘          └───────────┘  │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │     sentinel-agent-gateway  (Traefik v3)               │ │
│  │     :8443  mTLS agent channel                          │ │
│  │     :4444  gRPC dataplane                              │ │
│  └────────────────────────────────────────────────────────┘ │
└──────────────────────────────┬───────────────────────────────┘
                               │
                    mTLS / gRPC (TLS)
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
   ┌────▼────┐           ┌─────▼────┐           ┌─────▼────┐
   │  Agent  │           │  Agent   │           │  Agent   │
   │ Windows │           │  Linux   │           │  macOS   │
   └─────────┘           └──────────┘           └──────────┘
```

## Technology Stack

| Component | Technology |
|-----------|------------|
| Frontend | React 18, TypeScript, Vite, Tailwind CSS |
| Backend | Go 1.22, Gin Framework |
| Database | PostgreSQL 16 |
| Cache | Redis 7 |
| Agent | Go 1.21 |
| Agent Gateway | Traefik v3 (`sentinel-agent-gateway`, :8443 mTLS, :4444 gRPC) |
| Edge Web Routing | `infra-traefik` (separate `~/infra/` stack) via shared `edge` network |
| Container | Docker, Docker Compose |

## Quick Start

### Prerequisites

- Docker and Docker Compose
- Git

### Development Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/yourusername/sentinel.git
   cd sentinel
   ```

2. Copy the environment template:
   ```bash
   cp .env.example .env
   ```

3. Start the development environment:
   ```bash
   make dev
   ```

4. Access the dashboard at `http://localhost:5173`

### First Run

On first startup with an empty database, Sentinel generates a random admin password:

```
========================================
FIRST RUN: Admin credentials
Email: admin@sentinel.local
Password: [randomly generated]
CHANGE THIS IMMEDIATELY
========================================
```

> **Important:** Save this password -- it is only shown once. Change it immediately after first login.

## Production Deployment

### Oracle Cloud Free Tier (Recommended)

Sentinel is designed to run within Oracle Cloud's Always Free tier:

1. Create an Oracle Cloud account
2. Launch an ARM Ampere A1 VM (up to 4 OCPUs, 24GB RAM free)
3. Configure security lists for ports 80 and 443
4. Run the deployment script:

```bash
export DOMAIN=your-domain.com
export EMAIL=your-email@example.com
sudo ./scripts/deploy.sh
```

### Manual Deployment

1. Set environment variables:
   ```bash
   export DOMAIN=your-domain.com
   export ACME_EMAIL=your-email@example.com
   export POSTGRES_PASSWORD=$(openssl rand -base64 32)
   export JWT_SECRET=$(openssl rand -base64 64)
   export ENROLLMENT_TOKEN=$(openssl rand -base64 32)
   ```

2. Generate the Redis config (required before the first start):
   ```bash
   bash scripts/generate-redis-conf.sh
   ```
   `REDIS_PASSWORD` (64 hex chars, `openssl rand -hex 32`) must be set in `.env`
   first — compose has no default for it and will refuse to start without it.
   The generator renders `configs/redis/redis.conf`, which holds `requirepass`
   so the password never reaches the redis-server argv or the Docker event
   stream. The file is gitignored and must be regenerated on every host and
   after every password change. See `configs/redis/README.md`.

3. Start the services:
   ```bash
   docker-compose up -d
   ```

To rotate the Redis password later, use `bash scripts/rotate-redis-password.sh`
— it updates `.env`, regenerates the config, recreates `redis` and `backend`
together (the backend fails fast on a Redis auth error, so they must cut over as
one), verifies, and rolls back automatically on failure.

## Agent Installation

### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri "https://your-server/agent/windows" -OutFile sentinel-agent.exe
.\sentinel-agent.exe install --server=https://your-server --token=YOUR_ENROLLMENT_TOKEN
```

### Linux

```bash
curl -sSL https://your-server/agent/linux -o sentinel-agent
chmod +x sentinel-agent
sudo ./sentinel-agent install --server=https://your-server --token=YOUR_ENROLLMENT_TOKEN
```

### macOS

```bash
curl -sSL https://your-server/agent/macos -o sentinel-agent
chmod +x sentinel-agent
sudo ./sentinel-agent install --server=https://your-server --token=YOUR_ENROLLMENT_TOKEN
```

## Building the Agent

### Current Platform

```bash
cd agent && go build -o sentinel-agent ./cmd/sentinel-agent
```

### All Platforms (requires cross)

```bash
make agent-all
```

## API Documentation

The API is RESTful and uses JSON. All authenticated endpoints require a Bearer token.

### Authentication

```bash
# Login
curl -X POST https://your-server/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@sentinel.local", "password": "admin"}'

# Response
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "uuid",
    "email": "admin@sentinel.local",
    "role": "admin"
  }
}
```

### Devices

```bash
# List devices
curl https://your-server/api/devices \
  -H "Authorization: Bearer YOUR_TOKEN"

# Get device details
curl https://your-server/api/devices/{id} \
  -H "Authorization: Bearer YOUR_TOKEN"

# Execute command
curl -X POST https://your-server/api/devices/{id}/commands \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"commandType": "shell", "payload": {"command": "hostname"}}'
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DOMAIN` | Server domain name | localhost |
| `DATABASE_URL` | PostgreSQL connection string | Required |
| `REDIS_URL` | Redis connection string | redis://redis:6379 |
| `JWT_SECRET` | JWT signing secret | Required |
| `ENROLLMENT_TOKEN` | Agent enrollment token | Required |
| `ACME_EMAIL` | Email for Let's Encrypt | - |

## Project Structure

```
sentinel/
├── agent/              # Go agent source
├── frontend/           # React frontend
├── server/             # Go backend
│   ├── cmd/           # Entry points
│   ├── internal/      # Internal packages
│   │   ├── api/      # HTTP handlers
│   │   ├── models/   # Data models
│   │   ├── middleware/
│   │   └── websocket/
│   └── pkg/          # Shared packages
├── migrations/         # Database migrations
├── scripts/           # Deployment scripts
├── docker-compose.yml # Container orchestration
└── Makefile          # Build automation
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `make test`
5. Submit a pull request

## License

MIT License - see LICENSE file for details.

## Support

- Documentation: https://docs.sentinel.example.com
- Issues: https://github.com/yourusername/sentinel/issues

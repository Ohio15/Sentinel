# Sentinel RMM - Oracle Cloud Architecture

## Overview

Sentinel is a cloud-hosted Remote Monitoring and Management (RMM) platform designed to run on Oracle Cloud's Always Free tier.

## Infrastructure Layout

```
┌────────────────────────────────────────────────────────────────────────────┐
│                    Oracle Cloud Always Free Tier                            │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        VM 1 (ARM - 12GB RAM)                         │   │
│  │                         Application Server                           │   │
│  │                                                                      │   │
│  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                │   │
│  │   │   Traefik   │  │   Backend   │  │  Frontend   │                │   │
│  │   │   (Proxy)   │  │    (Go)     │  │  (React)    │                │   │
│  │   │   :80/443   │  │   :8080     │  │   (static)  │                │   │
│  │   └──────┬──────┘  └──────┬──────┘  └─────────────┘                │   │
│  │          │                │                                         │   │
│  │          │         ┌──────┴──────┐                                 │   │
│  │          │         │  WebSocket  │                                 │   │
│  │          │         │    Hub      │                                 │   │
│  │          │         │   :8081     │                                 │   │
│  │          │         └─────────────┘                                 │   │
│  └──────────┼──────────────────────────────────────────────────────────┘   │
│             │                                                               │
│  ┌──────────┼──────────────────────────────────────────────────────────┐   │
│  │          │          VM 2 (ARM - 12GB RAM)                           │   │
│  │          │           Database Server                                 │   │
│  │          │                                                          │   │
│  │          │    ┌─────────────┐    ┌─────────────┐                   │   │
│  │          └───►│ PostgreSQL  │    │    Redis    │                   │   │
│  │               │    :5432    │    │    :6379    │                   │   │
│  │               │             │    │  (cache/    │                   │   │
│  │               │             │    │   pubsub)   │                   │   │
│  │               └─────────────┘    └─────────────┘                   │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ HTTPS/WSS (TLS)
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        │                           │                           │
┌───────▼───────┐          ┌───────▼───────┐          ┌───────▼───────┐
│  Agent (Rust) │          │  Agent (Rust) │          │  Agent (Rust) │
│   Windows PC  │          │  Linux Server │          │   macOS Mac   │
└───────────────┘          └───────────────┘          └───────────────┘
```

## Technology Stack

| Component | Technology | Purpose |
|-----------|------------|---------|
| **Reverse Proxy** | Traefik v3 | SSL termination, routing, Let's Encrypt |
| **Backend API** | Go 1.22 + Gin | REST API, business logic |
| **WebSocket Hub** | Go + gorilla/websocket | Real-time agent communication |
| **Database** | PostgreSQL 16 | Primary data store |
| **Cache/PubSub** | Redis 7 | Session cache, real-time events |
| **Frontend** | React 18 + Vite | Admin web interface |
| **Agent** | Rust | Cross-platform endpoint agent |
| **Containerization** | Docker + Compose | Deployment orchestration |

## Oracle Cloud Free Tier Resources

### What We Get (Always Free)
- **2 AMD VMs** (1 OCPU, 1GB RAM each) OR
- **4 ARM VMs** (24GB RAM, 4 OCPUs total) ← We'll use this
- **200GB Block Storage** (boot volumes)
- **10TB/month Outbound Data**
- **2 Autonomous Databases** (optional, 20GB each)
- **Object Storage** (10GB)

### Our Allocation
| Resource | VM 1 (App) | VM 2 (DB) |
|----------|------------|-----------|
| Shape | VM.Standard.A1.Flex | VM.Standard.A1.Flex |
| OCPUs | 2 | 2 |
| RAM | 12 GB | 12 GB |
| Boot Volume | 50 GB | 100 GB |
| Purpose | App + Proxy | PostgreSQL + Redis |

## Network Architecture

```
Internet
    │
    ▼
┌─────────────────────────────────────┐
│     Oracle Cloud VCN (10.0.0.0/16)  │
│                                      │
│  ┌────────────────────────────────┐ │
│  │    Public Subnet (10.0.1.0/24) │ │
│  │                                 │ │
│  │  ┌──────────┐   ┌──────────┐  │ │
│  │  │   VM 1   │   │   VM 2   │  │ │
│  │  │ 10.0.1.2 │   │ 10.0.1.3 │  │ │
│  │  │          │   │          │  │ │
│  │  │ Public IP│   │ (Private)│  │ │
│  │  └──────────┘   └──────────┘  │ │
│  └────────────────────────────────┘ │
└─────────────────────────────────────┘

Security Lists:
- Ingress: 80, 443 (VM1 only)
- Ingress: 22 (SSH, your IP only)
- Internal: All traffic between VMs
- Egress: All allowed
```

## Service Architecture

### Backend Services (Single Go Binary)

For simplicity on free tier, we use a monolithic Go application with internal modules:

```
sentinel-server/
├── cmd/
│   └── server/
│       └── main.go           # Entry point
├── internal/
│   ├── api/                  # HTTP handlers
│   │   ├── router.go
│   │   ├── auth.go
│   │   ├── devices.go
│   │   ├── commands.go
│   │   ├── scripts.go
│   │   ├── alerts.go
│   │   └── settings.go
│   ├── websocket/            # WebSocket hub
│   │   ├── hub.go
│   │   ├── client.go
│   │   └── protocol.go
│   ├── services/             # Business logic
│   │   ├── auth.go
│   │   ├── device.go
│   │   ├── command.go
│   │   ├── script.go
│   │   ├── alert.go
│   │   └── metrics.go
│   ├── models/               # Data models
│   ├── repository/           # Database access
│   └── middleware/           # HTTP middleware
├── pkg/
│   ├── config/
│   ├── database/
│   └── cache/
└── go.mod
```

### API Endpoints

```
Authentication:
  POST   /api/auth/login
  POST   /api/auth/logout
  POST   /api/auth/refresh
  GET    /api/auth/me

Devices:
  GET    /api/devices
  GET    /api/devices/:id
  DELETE /api/devices/:id
  GET    /api/devices/:id/metrics
  POST   /api/devices/:id/command

Commands:
  GET    /api/commands
  GET    /api/commands/:id
  POST   /api/commands

Scripts:
  GET    /api/scripts
  POST   /api/scripts
  GET    /api/scripts/:id
  PUT    /api/scripts/:id
  DELETE /api/scripts/:id
  POST   /api/scripts/:id/execute

Alerts:
  GET    /api/alerts
  GET    /api/alerts/:id
  POST   /api/alerts/:id/acknowledge
  POST   /api/alerts/:id/resolve

Alert Rules:
  GET    /api/alert-rules
  POST   /api/alert-rules
  PUT    /api/alert-rules/:id
  DELETE /api/alert-rules/:id

Settings:
  GET    /api/settings
  PUT    /api/settings

Agent Enrollment:
  POST   /api/agent/enroll

WebSocket:
  GET    /ws/agent      # Agent connections
  GET    /ws/dashboard  # Dashboard real-time updates
```

## Database Schema

See `migrations/` folder for full schema. Key tables:

- `users` - Admin users
- `devices` - Registered agents/endpoints
- `device_metrics` - Time-series metrics (partitioned)
- `commands` - Command execution history
- `scripts` - Script library
- `alerts` - Alert instances
- `alert_rules` - Alert rule definitions
- `sessions` - User sessions
- `audit_log` - Audit trail

## Security Model

### Authentication
- JWT tokens (access + refresh)
- Secure HTTP-only cookies for web
- API keys for programmatic access

### Agent Authentication
1. Initial enrollment with one-time token
2. Agent receives unique credentials
3. Subsequent connections use agent credentials
4. TLS for all communication

### Authorization
- Role-based: Admin, Operator, Viewer
- Resource-level permissions

## Deployment

### Prerequisites
1. Oracle Cloud account (free)
2. Domain name (optional, can use IP)
3. SSH key pair

### Quick Start
```bash
# 1. Clone repository
git clone https://github.com/yourusername/sentinel.git

# 2. Install and configure OCI CLI
# See: https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm
oci setup config

# 3. Run Oracle Cloud setup script (creates VMs, network, security)
# Optional: Set region with REGION=us-phoenix-1 ./scripts/oracle-setup.sh
./scripts/oracle-setup.sh

# 4. SSH to the App VM and deploy
ssh opc@<APP_PUBLIC_IP>
cd /opt/sentinel
sudo ./scripts/deploy.sh
```

### Oracle Setup Script

The `scripts/oracle-setup.sh` script automates Oracle Cloud infrastructure provisioning:

**What it creates:**
- VCN (Virtual Cloud Network) with CIDR 10.0.0.0/16
- Public subnet (10.0.1.0/24) with internet gateway
- Security list (SSH, HTTP, HTTPS, internal traffic)
- App VM (2 OCPUs, 12GB RAM, 50GB storage) - public IP
- DB VM (2 OCPUs, 12GB RAM, 100GB storage) - internal only

**Environment variables:**
| Variable | Default | Description |
|----------|---------|-------------|
| `REGION` | us-ashburn-1 | Oracle Cloud region |
| `COMPARTMENT_ID` | (tenancy root) | Target compartment |
| `SSH_PUBLIC_KEY_FILE` | ~/.ssh/id_rsa.pub | SSH key for VM access |

**Usage:**
```bash
# Default region (us-ashburn-1)
./scripts/oracle-setup.sh

# Custom region
REGION=us-phoenix-1 ./scripts/oracle-setup.sh

# Custom compartment
COMPARTMENT_ID=ocid1.compartment.oc1..xxx ./scripts/oracle-setup.sh
```

The script is idempotent - it checks for existing resources before creating new ones.

## Scaling Considerations

While designed for free tier, the architecture supports scaling:

1. **Horizontal API scaling** - Add more app containers
2. **Read replicas** - PostgreSQL streaming replication
3. **Redis cluster** - For larger deployments
4. **CDN** - Cloudflare free tier for static assets

## Monitoring

Built-in monitoring (no external dependencies):
- `/health` - Health check endpoint
- `/metrics` - Prometheus-compatible metrics
- Dashboard shows system stats
- Alert on infrastructure issues

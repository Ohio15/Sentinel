# TLS/mTLS Implementation for Sentinel RMM

This document describes the TLS/mTLS security implementation for Sentinel RMM, covering both gRPC and WebSocket communications.

## Overview

Sentinel RMM now supports TLS encryption for:
- **gRPC Server** (port 8082) - Agent data plane communication
- **WebSocket Server** (port 8081) - Real-time agent communication
- **HTTPS Server** (port 8081) - REST API endpoints

All components support graceful fallback to insecure mode if certificates are not available.

## Quick Start

### 1. Generate Self-Signed Certificates

For development and testing, generate self-signed certificates:

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1

# With custom parameters
powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1 -Hostname "myserver.local" -ValidityDays 730
```

This creates:
- `certs/ca-cert.pem` - Certificate Authority certificate
- `certs/ca-key.pem` - CA private key (keep secure!)
- `certs/server-cert.pem` - Server certificate
- `certs/server-key.pem` - Server private key (keep secure!)

### 2. Start the Server

The server automatically detects and uses TLS certificates:

```bash
npm run build
npm start
```

Check the logs for:
```
[TLS] TLS certificates loaded successfully
gRPC DataPlane server listening on port 8082 (TLS)
Server listening on port 8081 (HTTPS)
```

### 3. Deploy Certificates to Agents

Copy `ca-cert.pem` to each agent machine:

**Windows:**
```powershell
# Copy to agent installation directory
copy certs\ca-cert.pem "C:\Program Files\Sentinel\ca-cert.pem"
```

**Linux:**
```bash
sudo mkdir -p /etc/sentinel/certs
sudo cp certs/ca-cert.pem /etc/sentinel/certs/ca-cert.pem
```

**macOS:**
```bash
sudo mkdir -p /usr/local/sentinel/certs
sudo cp certs/ca-cert.pem /usr/local/sentinel/certs/ca-cert.pem
```

### 4. Configure Agents

Agents automatically search for CA certificates in these locations:

**Windows:**
- `<agent-executable-dir>\ca-cert.pem`
- `<agent-executable-dir>\certs\ca-cert.pem`
- `C:\ProgramData\Sentinel\certs\ca-cert.pem`

**Linux:**
- `<agent-executable-dir>/ca-cert.pem`
- `<agent-executable-dir>/certs/ca-cert.pem`
- `/etc/sentinel/certs/ca-cert.pem`

**macOS:**
- `<agent-executable-dir>/ca-cert.pem`
- `<agent-executable-dir>/certs/ca-cert.pem`
- `/usr/local/sentinel/certs/ca-cert.pem`

## Architecture

### gRPC Server (TypeScript)

**File:** `src/main/grpc-server.ts`

**Key Features:**
- Configurable TLS via constructor: `new GrpcServer(db, agentMgr, port, useTLS)`
- Automatic certificate loading from `certs/` directory
- Support for mTLS (client certificate verification)
- Graceful fallback to insecure mode

**TLS Configuration:**
```typescript
const server = new GrpcServer(database, agentManager, 8082, true); // true = use TLS
await server.start();
```

**Certificate Paths:**
- Development: `<project-root>/certs/`
- Production: `<resources>/certs/`

### gRPC Client (Go)

**File:** `agent/internal/grpc/dataplane.go`

**Key Features:**
- Automatic TLS detection and configuration
- CA certificate auto-discovery
- Fallback to insecure connection if CA not found
- Configurable TLS settings

**Usage:**
```go
// Auto-detect TLS (default)
client := grpc.NewDataPlaneClient(agentID, serverAddress)

// Explicit TLS configuration
client := grpc.NewDataPlaneClientWithTLS(agentID, serverAddress, true, "/path/to/ca-cert.pem")

// Disable TLS
client := grpc.NewDataPlaneClientWithTLS(agentID, serverAddress, false, "")
```

### WebSocket/HTTPS Server (TypeScript)

**File:** `src/main/server.ts`
**Helper:** `src/main/tls-config.ts`

**Key Features:**
- Dual HTTP/HTTPS server support
- Automatic WSS upgrade when TLS is enabled
- Shared certificate management with gRPC
- Graceful fallback to HTTP/WS

**Implementation:**
See `HTTPS_WSS_UPGRADE.md` for integration guide.

## Certificate Management

### Development Certificates

The provided script generates self-signed certificates suitable for development:

```powershell
.\scripts\generate-certs.ps1
```

**Generated Certificate Details:**
- **Algorithm:** RSA 4096-bit
- **Validity:** 365 days (configurable)
- **Subject Alternative Names (SANs):**
  - DNS: localhost, *.local, <hostname>
  - IP: 127.0.0.1, ::1

### Production Certificates

For production deployments, use certificates from a trusted Certificate Authority (CA):

1. **Obtain certificates:**
   - Purchase from commercial CA (DigiCert, Let's Encrypt, etc.)
   - Use internal PKI if available
   - Consider using cert-manager for Kubernetes deployments

2. **Install certificates:**
   ```bash
   # Copy to certs directory
   cp /path/to/server.crt certs/server-cert.pem
   cp /path/to/server.key certs/server-key.pem
   cp /path/to/ca.crt certs/ca-cert.pem
   ```

3. **Set proper permissions:**
   ```bash
   # Linux/macOS
   chmod 600 certs/*.key
   chmod 644 certs/*.pem

   # Windows
   # Use File Properties > Security > Advanced
   # Remove all users except SYSTEM and current user
   ```

### Certificate Rotation

To rotate certificates:

1. Generate or obtain new certificates
2. Replace files in `certs/` directory
3. Restart Sentinel server
4. Update agents with new CA certificate (if changed)
5. Restart agents

**Automated rotation (recommended for production):**
- Use cert-manager or similar tools
- Implement certificate expiry monitoring
- Set up automated renewal (e.g., Let's Encrypt ACME)

## Security Considerations

### Current Implementation

**✅ Implemented:**
- TLS 1.2+ encryption for all gRPC connections
- TLS 1.2+ encryption for WebSocket connections
- Server certificate validation
- Automatic secure fallback
- Private key protection

**⚠️ Optional (configure as needed):**
- mTLS (mutual TLS) - client certificate verification
- Certificate pinning
- OCSP stapling
- Certificate transparency

### Enabling mTLS

To enable mutual TLS (client certificate authentication):

1. **Generate client certificates:**
   ```powershell
   # Modify generate-certs.ps1 to create client certs
   # Or use openssl manually
   ```

2. **Update gRPC server:**
   ```typescript
   // In grpc-server.ts, line 124
   false // Change to: true
   ```

3. **Update agents with client certificates**

4. **Configure client to present certificates**

### Best Practices

1. **Never commit private keys to version control**
   - Add `certs/*.key` to `.gitignore`
   - Add `certs/*.pem` to `.gitignore` (except example certs)

2. **Use strong key sizes**
   - Minimum 2048-bit RSA (4096-bit recommended)
   - Consider ECDSA P-256 or P-384 for better performance

3. **Implement certificate monitoring**
   - Monitor certificate expiration
   - Set up alerts 30 days before expiry
   - Automate renewal process

4. **Restrict certificate access**
   - Store private keys in secure key storage (HSM, vault)
   - Use minimal file permissions
   - Never expose over network shares

5. **Use proper hostnames**
   - Include all hostnames in SANs
   - Avoid wildcard certificates in production
   - Use FQDN instead of IP addresses

## Troubleshooting

### "TLS certificates not found"

**Problem:** Server can't find certificate files

**Solution:**
```powershell
# Verify certificates exist
dir certs\

# Expected files:
# ca-cert.pem, ca-key.pem, server-cert.pem, server-key.pem

# Regenerate if missing
powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1
```

### "Certificate verify failed" on agent

**Problem:** Agent can't verify server certificate

**Solutions:**
1. Ensure `ca-cert.pem` is deployed to agent
2. Check CA certificate path in agent logs
3. Verify server certificate is signed by the CA:
   ```bash
   openssl verify -CAfile certs/ca-cert.pem certs/server-cert.pem
   ```

### "Connection refused" after enabling TLS

**Problem:** gRPC client trying HTTP on HTTPS port

**Solution:**
- Ensure both server and client have matching TLS settings
- Check server logs for TLS status
- Verify agent has CA certificate

### Self-signed certificate warnings

**Problem:** Browsers/tools warning about self-signed certs

**Solutions:**
1. **For development:** Add CA to trusted root certificates
   ```powershell
   # Windows (run as Administrator)
   Import-Certificate -FilePath "certs\ca-cert.pem" -CertStoreLocation Cert:\LocalMachine\Root
   ```

2. **For production:** Use certificates from trusted CA

### Performance issues with TLS

**Problem:** Increased latency after enabling TLS

**Solutions:**
- Enable HTTP/2 for gRPC (enabled by default)
- Use session resumption
- Consider ECDSA certificates (faster than RSA)
- Enable TLS offloading if using load balancer

## Monitoring and Logging

### Server Logs

Look for these log messages:

**Successful TLS:**
```
[TLS] TLS certificates loaded successfully
gRPC server configured with TLS (server-side only)
gRPC DataPlane server listening on port 8082 (TLS)
Server listening on port 8081 (HTTPS)
```

**Fallback to insecure:**
```
[TLS] Certificates not found, falling back to HTTP/WS
WARNING: gRPC server running in INSECURE mode (no TLS)
gRPC DataPlane server listening on port 8082 (insecure)
Server listening on port 8081 (HTTP)
```

### Agent Logs

Look for these log messages:

**Successful TLS:**
```
[gRPC] Found CA certificate at: /path/to/ca-cert.pem
[gRPC] TLS enabled with CA certificate: /path/to/ca-cert.pem
[gRPC] Connected to Data Plane at <server>:8082 (TLS)
```

**Fallback to insecure:**
```
[gRPC] WARNING: No CA certificate found, using insecure connection
[gRPC] WARNING: Using insecure connection (no TLS)
[gRPC] Connected to Data Plane at <server>:8082 (insecure)
```

## Upgrade Path

### From Insecure to TLS

1. Generate certificates on server
2. Restart server (will use TLS if certs found)
3. Deploy CA cert to agents gradually
4. Monitor mixed secure/insecure connections
5. Once all agents upgraded, consider enforcing TLS

### From TLS to mTLS

1. Generate client certificates
2. Enable client verification on server
3. Deploy client certs to agents
4. Update agent configuration
5. Restart agents

## File Locations Summary

### Server (TypeScript)
- **Development:** `<project-root>/certs/`
- **Production:** `<resources>/certs/`
- **Config:** `src/main/tls-config.ts`

### Agent (Go)
- **Windows:** `C:\ProgramData\Sentinel\certs\ca-cert.pem`
- **Linux:** `/etc/sentinel/certs/ca-cert.pem`
- **macOS:** `/usr/local/sentinel/certs/ca-cert.pem`
- **Config:** `agent/internal/grpc/dataplane.go`

## References

- [gRPC Authentication Guide](https://grpc.io/docs/guides/auth/)
- [Node.js TLS/SSL](https://nodejs.org/api/tls.html)
- [Go crypto/tls Package](https://pkg.go.dev/crypto/tls)
- [OpenSSL Documentation](https://www.openssl.org/docs/)

## Support

For issues or questions:
1. Check server/agent logs for TLS-related messages
2. Verify certificate paths and permissions
3. Test certificate validity with openssl
4. Review this documentation
5. Open an issue with logs and configuration details

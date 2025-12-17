# TLS Implementation Checklist

Use this checklist to verify your TLS implementation is complete and working correctly.

## Initial Setup

### Certificate Generation
- [ ] PowerShell script created at `scripts/generate-certs.ps1`
- [ ] Run script successfully: `powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1`
- [ ] Verify certificates created in `certs/` directory:
  - [ ] `ca-cert.pem` exists
  - [ ] `ca-key.pem` exists
  - [ ] `server-cert.pem` exists
  - [ ] `server-key.pem` exists
- [ ] Private keys have restricted permissions (Windows: admin only, Linux: 600)

### Server Configuration

#### gRPC Server
- [ ] `src/main/grpc-server.ts` updated with TLS support
- [ ] Added imports: `fs`, `app` from electron
- [ ] Added `useTLS` parameter to constructor
- [ ] Implemented `createServerCredentials()` method
- [ ] Replaced `createInsecure()` with `createServerCredentials()`
- [ ] Certificate loading logic implemented
- [ ] Graceful fallback to insecure mode implemented
- [ ] Logging shows TLS status

#### WebSocket/HTTPS Server
- [ ] `src/main/tls-config.ts` helper module created
- [ ] Helper functions implemented:
  - [ ] `loadTLSCertificates()`
  - [ ] `createSecureServer()`
  - [ ] `getWebSocketProtocol()`
  - [ ] `checkTLSCertificates()`
- [ ] `src/main/server.ts` integration (see `HTTPS_WSS_UPGRADE.md`)
  - [ ] Import `https` module
  - [ ] Import TLS helper functions
  - [ ] Update server creation to use `createSecureServer()`
  - [ ] Update protocol references (http → https, ws → wss)

#### Agent Client
- [ ] `agent/internal/grpc/dataplane.go` updated with TLS support
- [ ] Added imports: `crypto/tls`, `crypto/x509`, `os`, `filepath`
- [ ] Added fields: `useTLS`, `caCertPath`
- [ ] Implemented `NewDataPlaneClientWithTLS()` constructor
- [ ] Implemented `findCACertificate()` function
- [ ] Implemented `createTransportCredentials()` method
- [ ] Updated `Connect()` to use TLS credentials
- [ ] CA certificate auto-detection working
- [ ] Graceful fallback to insecure mode implemented

### Documentation
- [ ] `TLS_IMPLEMENTATION.md` - Complete technical documentation
- [ ] `TLS_QUICKSTART.md` - Quick start guide
- [ ] `TLS_SUMMARY.md` - Implementation summary
- [ ] `TLS_ARCHITECTURE.md` - Architecture diagrams
- [ ] `HTTPS_WSS_UPGRADE.md` - WebSocket integration guide
- [ ] `certs/README.md` - Certificate directory guide
- [ ] `agent/config.example.yaml` - Agent configuration example
- [ ] `.gitignore` updated to exclude certificates

## Testing

### Build Tests
- [ ] TypeScript server builds without errors: `npm run build`
- [ ] Go agent builds without errors: `cd agent && go build`
- [ ] No certificate-related compilation errors

### Server Tests
- [ ] Server starts successfully
- [ ] Check logs for TLS initialization:
  - [ ] See: `[TLS] TLS certificates loaded successfully`
  - [ ] See: `gRPC server configured with TLS`
  - [ ] See: `gRPC DataPlane server listening on port 8082 (TLS)`
  - [ ] See: `Server listening on port 8081 (HTTPS)`
- [ ] Server gracefully falls back without certificates:
  - [ ] Rename `certs/` directory temporarily
  - [ ] Start server
  - [ ] See: `[TLS] Certificates not found, falling back`
  - [ ] See: `WARNING: gRPC server running in INSECURE mode`
  - [ ] Restore `certs/` directory

### Agent Tests
- [ ] Deploy CA certificate to test agent machine
- [ ] Agent finds CA certificate:
  - [ ] Check logs: `[gRPC] Found CA certificate at: <path>`
- [ ] Agent connects with TLS:
  - [ ] Check logs: `[gRPC] TLS enabled with CA certificate`
  - [ ] Check logs: `[gRPC] Connected to Data Plane at <server>:8082 (TLS)`
- [ ] Agent gracefully falls back without CA cert:
  - [ ] Remove CA certificate
  - [ ] Start agent
  - [ ] See: `[gRPC] WARNING: No CA certificate found`
  - [ ] See: `[gRPC] WARNING: Using insecure connection`
  - [ ] Restore CA certificate

### Integration Tests
- [ ] Agent successfully enrolls over TLS
- [ ] Metrics streaming works over TLS
- [ ] Inventory upload works over TLS
- [ ] WebSocket connection upgrades to WSS
- [ ] Terminal sessions work over WSS
- [ ] File transfers work over gRPC TLS
- [ ] No certificate errors in logs

### Certificate Validation Tests
- [ ] Verify server certificate is valid:
  ```bash
  openssl x509 -in certs/server-cert.pem -noout -text
  ```
- [ ] Verify certificate is signed by CA:
  ```bash
  openssl verify -CAfile certs/ca-cert.pem certs/server-cert.pem
  ```
- [ ] Verify certificate has correct SANs:
  ```bash
  openssl x509 -in certs/server-cert.pem -noout -text | grep -A1 "Subject Alternative Name"
  ```
- [ ] Verify certificate expiration:
  ```bash
  openssl x509 -in certs/server-cert.pem -noout -dates
  ```

## Security Verification

### Access Control
- [ ] Private keys have restrictive permissions
- [ ] Certificates are not committed to git (check `.gitignore`)
- [ ] Private keys are not world-readable (Linux/macOS)
- [ ] Private keys are not accessible to non-admin users (Windows)

### Encryption Verification
- [ ] Capture network traffic with Wireshark/tcpdump
- [ ] Verify gRPC traffic is encrypted (should see TLS handshake)
- [ ] Verify WebSocket traffic is encrypted (WSS protocol)
- [ ] Cannot read plaintext data in packet capture

### Certificate Chain Verification
- [ ] Server presents correct certificate chain
- [ ] Agent validates server certificate
- [ ] Certificate hostname matches server hostname
- [ ] No certificate warnings in agent logs

## Deployment Checklist

### Pre-Deployment
- [ ] Certificates generated for production environment
- [ ] Private keys secured in proper storage
- [ ] CA certificate prepared for distribution
- [ ] Documentation reviewed and updated
- [ ] Rollback plan prepared

### Server Deployment
- [ ] Build server with TLS support
- [ ] Copy certificates to production server
- [ ] Verify certificate paths are correct
- [ ] Set proper file permissions
- [ ] Start server and verify TLS initialization
- [ ] Monitor logs for errors
- [ ] Test health endpoint over HTTPS

### Agent Deployment
- [ ] Distribute CA certificate to agent machines
- [ ] Deploy to standard location or configure custom path
- [ ] Rebuild agents with TLS support
- [ ] Deploy agents to test group first
- [ ] Verify successful TLS connections
- [ ] Monitor agent logs
- [ ] Gradual rollout to all agents

### Post-Deployment
- [ ] All agents connecting over TLS
- [ ] No certificate errors in logs
- [ ] Performance monitoring shows acceptable overhead
- [ ] Network traffic is encrypted
- [ ] Document actual deployment procedures
- [ ] Schedule certificate rotation

## Monitoring

### Server Monitoring
- [ ] Monitor TLS handshake errors
- [ ] Monitor certificate expiration dates
- [ ] Monitor fallback to insecure mode
- [ ] Set up alerts for certificate expiry (30 days)
- [ ] Monitor TLS version usage
- [ ] Monitor cipher suite usage

### Agent Monitoring
- [ ] Monitor agent TLS connection status
- [ ] Monitor certificate validation failures
- [ ] Monitor fallback to insecure connections
- [ ] Track agents not using TLS
- [ ] Alert on insecure connections

## Maintenance

### Certificate Rotation Plan
- [ ] Document certificate rotation procedure
- [ ] Set up calendar reminders for rotation
- [ ] Test rotation procedure in dev environment
- [ ] Prepare new certificates before current expire
- [ ] Plan distribution of new certificates
- [ ] Schedule rotation maintenance window

### Ongoing Tasks
- [ ] Monthly certificate expiration check
- [ ] Quarterly security review
- [ ] Keep TLS libraries updated
- [ ] Review cipher suite configurations
- [ ] Update documentation as needed

## Optional Enhancements

### mTLS (Mutual TLS)
- [ ] Generate client certificates
- [ ] Enable client certificate verification on server
- [ ] Deploy client certificates to agents
- [ ] Update agent configuration
- [ ] Test mutual authentication
- [ ] Document mTLS procedures

### Advanced Security
- [ ] Implement certificate pinning
- [ ] Enable OCSP stapling
- [ ] Configure certificate transparency
- [ ] Set up HSM for private key storage
- [ ] Implement automated certificate renewal
- [ ] Configure certificate revocation checking

## Troubleshooting

### Common Issues Verified
- [ ] "TLS certificates not found" - Verified fix works
- [ ] "Certificate verify failed" - Verified fix works
- [ ] "Connection refused" - Verified fix works
- [ ] Self-signed warnings - Documented workarounds
- [ ] Performance issues - Benchmarked and acceptable

## Documentation Review

### User Documentation
- [ ] TLS_QUICKSTART.md is clear and accurate
- [ ] TLS_IMPLEMENTATION.md is comprehensive
- [ ] Certificate generation instructions work
- [ ] Deployment instructions work
- [ ] Troubleshooting guide is helpful

### Developer Documentation
- [ ] Code comments are clear
- [ ] TLS_ARCHITECTURE.md explains design
- [ ] Integration points documented
- [ ] API changes documented
- [ ] Configuration options documented

## Compliance & Audit

### Security Best Practices
- [ ] Using TLS 1.2 or higher
- [ ] Strong cipher suites configured
- [ ] Forward secrecy enabled
- [ ] Certificate validation enabled
- [ ] Private keys properly secured
- [ ] Logging sufficient for audit

### Industry Standards
- [ ] OWASP recommendations followed
- [ ] NIST guidelines followed
- [ ] PCI DSS compliance (if applicable)
- [ ] HIPAA compliance (if applicable)
- [ ] SOC 2 requirements (if applicable)

## Sign-Off

### Development Team
- [ ] Code reviewed
- [ ] Security reviewed
- [ ] Tested in dev environment
- [ ] Documentation complete

### Operations Team
- [ ] Deployment procedures reviewed
- [ ] Monitoring configured
- [ ] Backup procedures updated
- [ ] Incident response plan updated

### Security Team
- [ ] Security review complete
- [ ] Penetration testing complete (if required)
- [ ] Compliance verification complete
- [ ] Risk assessment complete

## Final Verification

- [ ] All checklist items completed
- [ ] All tests passing
- [ ] Production deployment successful
- [ ] Monitoring active
- [ ] Documentation published
- [ ] Team trained

---

**Implementation Date:** _______________
**Verified By:** _______________
**Next Review Date:** _______________

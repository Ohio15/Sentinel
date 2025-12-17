# HTTPS/WSS Upgrade Guide for server.ts

## Changes Required

To enable HTTPS and WSS support in `src/main/server.ts`, make the following changes:

### 1. Add imports at the top of the file

Add this import after the existing imports:

```typescript
import { createSecureServer, loadTLSCertificates, getWebSocketProtocol } from './tls-config';
```

### 2. Update the Server class properties

Change:
```typescript
private server: http.Server | null = null;
```

To:
```typescript
private server: http.Server | https.Server | null = null;
private useTLS: boolean = true;
private tlsProtocol: 'http' | 'https' = 'http';
```

### 3. Update the start() method

Replace the server creation section in the `start()` method:

Find this code (around line 682-688):
```typescript
return new Promise((resolve, reject) => {
  this.server = this.app.listen(this.port, '0.0.0.0', () => {
    console.log(`Server listening on port ${this.port}`);
    this.setupWebSocket();
    resolve();
  });
```

Replace with:
```typescript
return new Promise((resolve, reject) => {
  // Create HTTP or HTTPS server based on TLS configuration
  const { server, protocol } = createSecureServer(this.app, { useTLS: this.useTLS });
  this.server = server;
  this.tlsProtocol = protocol;

  this.server.listen(this.port, '0.0.0.0', () => {
    console.log(`Server listening on port ${this.port} (${protocol.toUpperCase()})`);
    this.setupWebSocket();
    resolve();
  });
```

### 4. Update getAgentInstallerCommand() method

Find the method `getAgentInstallerCommand(platform: string)` and update the `serverUrl` construction:

Change:
```typescript
const serverUrl = `http://${localIp}:${this.port}`;
```

To:
```typescript
const serverUrl = `${this.tlsProtocol}://${localIp}:${this.port}`;
```

This should appear in multiple places in the method. Update all occurrences.

### 5. Update server info endpoint (if exists)

If you have an endpoint that returns server info, update it to return the correct WebSocket protocol:

```typescript
this.app.get('/api/server/info', (req: Request, res: Response) => {
  const localIp = this.getLocalIpAddress();
  const wsProtocol = getWebSocketProtocol(this.tlsProtocol === 'https');
  res.json({
    wsEndpoint: `${wsProtocol}://${localIp}:${this.port}/ws`,
    version: '1.0.0',
    secure: this.tlsProtocol === 'https',
  });
});
```

### 6. Update any other HTTP URL references

Search for other places where `http://` is hardcoded and replace with `${this.tlsProtocol}://`

## Testing

After making these changes:

1. Generate certificates:
   ```powershell
   powershell -ExecutionPolicy Bypass -File scripts/generate-certs.ps1
   ```

2. Build the project:
   ```bash
   npm run build
   ```

3. Start the server and verify in logs:
   - Should see: "Created HTTPS server with TLS"
   - Should see: "Server listening on port 8081 (HTTPS)"

4. Test WebSocket connection (will be WSS instead of WS)

## Graceful Fallback

The implementation automatically falls back to HTTP/WS if:
- Certificates are not found
- Certificate loading fails
- `useTLS` is set to `false`

This ensures the system continues to work even without certificates.

## Production Deployment

For production:
1. Use certificates from a trusted CA (not self-signed)
2. Set `useTLS: true` (default)
3. Ensure certificates are deployed to the `certs/` directory
4. Monitor logs for TLS-related warnings

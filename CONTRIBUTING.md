# Contributing to Sentinel

## Development Setup

### Prerequisites
- Node.js 20+
- Go 1.21+
- Windows (for full testing)

### Installation
```bash
npm install
cd agent && go mod download
```

## Code Quality Tools

### Go Agent

**Linting:**
```bash
cd agent
go vet ./...
staticcheck ./...
golangci-lint run ./...
```

**Testing:**
```bash
cd agent
go test -race ./...
```

**Build:**
```bash
cd agent
go build ./cmd/sentinel-agent
```

### Electron App

**Linting:**
```bash
npm run lint
npm run lint:fix  # Auto-fix issues
```

**Type checking:**
```bash
npx tsc -p tsconfig.main.json --noEmit
```

**Build:**
```bash
npm run build
```

## Common Issues to Avoid

### 1. Deadlocks in Go
Never call a locking function from within another lock:
```go
// BAD - will deadlock
func (c *Config) Load() {
    mu.Lock()
    defer mu.Unlock()
    c.Save()  // Save() also locks mu!
}

// GOOD - use internal unlocked version
func (c *Config) Load() {
    mu.Lock()
    defer mu.Unlock()
    c.saveUnlocked()  // Doesn't lock
}
```

### 2. Hardcoded Paths
Always use the `paths` package:
```go
// BAD
configPath := "C:\\ProgramData\\Sentinel\\config.json"

// GOOD
configPath := paths.ConfigPath()
```

### 3. Floating Promises in TypeScript
Always handle async operations:
```typescript
// BAD - promise result ignored
someAsyncFunction();

// GOOD
await someAsyncFunction();
// or
someAsyncFunction().catch(console.error);
```

### 4. IPC Handler Registration
Register IPC handlers before the renderer can call them:
```typescript
// Register handlers early in main.ts
function setupHandlers() {
    ipcMain.handle('my-handler', async () => { ... });
}

// Call before createWindow()
setupHandlers();
```

## Pull Request Process

1. Create a feature branch from `master`
2. Make your changes
3. Run linting and tests locally
4. Fill out the PR template checklist
5. Request review

## Commit Messages

Format: `<type>: <description>`

Types:
- `fix:` Bug fixes
- `feat:` New features
- `refactor:` Code changes that don't fix bugs or add features
- `docs:` Documentation changes
- `test:` Adding or updating tests
- `chore:` Maintenance tasks

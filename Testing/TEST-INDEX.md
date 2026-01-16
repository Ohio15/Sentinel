# Sentinel Testing Suite

## Overview

This directory contains all testing materials for the Sentinel RMM system, organized by component and test type.

## Directory Structure

```
Testing/
├── TEST-INDEX.md           # This file
├── Agent/
│   ├── FileTransfer/       # File transfer security tests
│   └── Executor/           # Command execution security tests
├── Server/
│   ├── Auth/               # Authentication middleware tests
│   └── Tokens/             # Token management tests
└── FullTests/              # Comprehensive multi-component tests
    ├── Test-SentinelMaster.ps1    # Master test orchestrator
    ├── Test-SentinelUpdate.ps1    # Update system tests
    ├── Test-SentinelRecovery.ps1  # Recovery mechanism tests
    └── Test-SentinelReport.ps1    # Report generator
```

## Test Categories

### Component Tests

| Component | Location | Description |
|-----------|----------|-------------|
| FileTransfer | `Agent/FileTransfer/` | Path traversal protection, symlink validation |
| Executor | `Agent/Executor/` | Command validation, rate limiting, blacklist enforcement |
| Auth | `Server/Auth/` | JWT validation, API key handling, role-based access |
| Tokens | `Server/Tokens/` | Enrollment token hashing, database validation |

### Full System Tests

| Test Script | Purpose |
|-------------|---------|
| `Test-SentinelMaster.ps1` | Orchestrates all tests, generates summary |
| `Test-SentinelUpdate.ps1` | Validates update mechanism, rollback capability |
| `Test-SentinelRecovery.ps1` | Tests watchdog, crash recovery, heartbeat |
| `Test-SentinelReport.ps1` | Generates HTML/JSON/Console reports |

## Running Tests

### Quick Start

```powershell
# Run all tests
cd D:\Projects\Sentinel\Testing\FullTests
.\Test-SentinelMaster.ps1

# Run with custom output path
.\Test-SentinelMaster.ps1 -OutputPath "C:\TestResults"

# Skip specific test suites
.\Test-SentinelMaster.ps1 -SkipUpdate -SkipRecovery
```

### Individual Test Suites

```powershell
# Update tests only
.\Test-SentinelUpdate.ps1

# Recovery tests with destructive testing enabled
.\Test-SentinelRecovery.ps1 -DestructiveTests -RecoveryTimeout 120

# Generate report from existing results
.\Test-SentinelReport.ps1 -OutputPath ".\TestResults" -Format HTML
```

## Security Tests

### CW-001: Path Traversal (CRITICAL)
- **File:** `Agent/FileTransfer/Test-PathTraversal.ps1`
- **Tests:**
  - Directory boundary validation
  - Symlink race condition prevention
  - Unicode/encoding bypass attempts

### CW-002: Command Execution
- **File:** `Agent/Executor/Test-CommandValidation.ps1`
- **Tests:**
  - Blacklist pattern enforcement
  - Rate limiting functionality
  - Concurrency limits
  - Path traversal in commands

### CW-003: Token Security
- **File:** `Server/Tokens/Test-TokenValidation.ps1`
- **Tests:**
  - Bcrypt hash validation
  - Legacy token backward compatibility
  - Expiration/usage limit enforcement

## Test Results

Results are exported to JSON files with timestamps:
- `test-results-YYYYMMDD-HHmmss.json` - Master results
- `update-test-results-YYYYMMDD-HHmmss.json` - Update tests
- `recovery-test-results-YYYYMMDD-HHmmss.json` - Recovery tests
- `test-report-YYYYMMDD-HHmmss.html` - HTML report

## Adding New Tests

1. Create test script in appropriate component directory
2. Follow naming convention: `Test-<Component><Feature>.ps1`
3. Export results as JSON with component identifier
4. Update this index with test description

## Prerequisites

- PowerShell 5.1 or higher
- Sentinel Agent installed (for agent tests)
- Network access to Sentinel server
- Administrator privileges (for service tests)

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | All tests passed |
| 1 | Some tests failed |
| 2 | Configuration error |
| 3 | Environment not ready |

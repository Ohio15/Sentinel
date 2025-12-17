## Summary

<!-- Brief description of what this PR does -->

## Changes

<!-- List the main changes in this PR -->
-

## Type of Change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to change)
- [ ] Refactoring (no functional changes)
- [ ] Documentation update

## Code Quality Checklist

### General
- [ ] Code follows the project's style guidelines
- [ ] Self-review of the code has been performed
- [ ] Comments added for complex logic
- [ ] No hardcoded values (use constants/config)

### Go Agent
- [ ] No hardcoded paths (use `paths` package)
- [ ] Mutex locks don't call other locking functions (no deadlock potential)
- [ ] Errors are wrapped with context (`fmt.Errorf("context: %w", err)`)
- [ ] `go vet` and `staticcheck` pass
- [ ] Unit tests added/updated for new functionality

### TypeScript/Electron
- [ ] TypeScript compiles without errors
- [ ] ESLint passes (or warnings are intentional)
- [ ] No floating promises (all async calls are awaited or handled)
- [ ] IPC handlers are registered before they can be called

### Security
- [ ] No secrets or credentials in code
- [ ] User input is validated
- [ ] File paths are sanitized (no path traversal)
- [ ] No SQL injection vulnerabilities

## Testing

<!-- Describe how this was tested -->
- [ ] Tested on Windows
- [ ] Agent service starts/stops correctly
- [ ] No console errors in Electron DevTools

## Related Issues

<!-- Link any related issues: Fixes #123, Relates to #456 -->

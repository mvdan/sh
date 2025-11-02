# mvdan/sh - Copilot Coding Agent Instructions

## Repository Overview

**mvdan/sh** is a Go library and toolset for shell script parsing, formatting, and interpretation. It supports POSIX Shell, Bash, and mksh. The repository provides:
- `shfmt`: A shell script formatter (the primary command-line tool)
- `gosh`: A proof-of-concept shell interpreter using the `interp` package
- Go packages for shell parsing (`syntax`), interpretation (`interp`), expansion (`expand`), pattern matching (`pattern`), and utilities

**Size**: ~34,000 lines of Go code across 8 main packages
**Language**: Pure Go (requires Go 1.24 or later)
**License**: BSD-3-Clause

## Building and Testing

### Environment Setup
- **Go Version**: 1.24.0 or later (CI tests with 1.24.x and 1.25.x)
- **No CGO required**: All code is pure Go
- **Cross-platform**: Works on Linux, macOS, Windows, and more

### Essential Commands

#### Install Dependencies (First Time)
```bash
go mod download
```
This happens automatically on first build/test, but you can run it explicitly. Downloads take ~5-10 seconds.

#### Build
```bash
# Build shfmt (main formatter tool)
go build ./cmd/shfmt

# Build gosh (interpreter demo)
go build ./cmd/gosh

# Build all packages without creating binaries
go build ./...
```
Build time: ~2-5 seconds for incremental builds, ~10-15 seconds for clean builds.

#### Run Tests

**ALWAYS run tests in this specific order to match CI:**

```bash
# 1. Basic tests (REQUIRED - run this first)
go test ./...
# Takes ~25-30 seconds, tests all packages

# 2. Tests in moreinterp subdirectory (REQUIRED)
cd moreinterp && go test ./...
# Takes ~7-10 seconds, has separate go.mod

# 3. Race detector tests (RECOMMENDED for concurrency changes)
go test -race ./...
# Takes ~40-50 seconds, only run on Linux in CI

# 4. 32-bit architecture test (OPTIONAL - for portability)
GOARCH=386 go test -count=1 ./...
# Takes ~15-20 seconds
```

**Note**: The `moreinterp` directory is a separate Go module with its own `go.mod`. Always test it separately with `cd moreinterp && go test ./...`

#### Linting and Formatting

```bash
# Check formatting (REQUIRED before commit)
gofmt -s -d .
# Must produce no output. If it does, your code needs formatting.

# Run go vet (REQUIRED before commit)
go vet ./...
# Must pass with no errors.

# Fix formatting issues
gofmt -s -w .
```

**CRITICAL**: CI enforces `gofmt -s` (simplified format). The diff check `diff <(echo -n) <(gofmt -s -d .)` must show no differences.

#### Fuzzing (Optional)
```bash
# Example: fuzz the parser/printer
cd syntax
go test -run=- -fuzz=FuzzParsePrint -fuzztime=30s
```

### Common Build Issues and Workarounds

1. **Missing Dependencies**: Run `go mod download` explicitly if tests fail with missing packages
2. **Stale Test Cache**: If tests behave unexpectedly, run `go test -count=1 ./...` to disable caching
3. **moreinterp Tests**: Must be run separately with `cd moreinterp && go test ./...` - they won't run from the root
4. **Cross-platform**: Some tests (like those using bash or pty) may be skipped on Windows

## Project Structure

### Repository Layout

```
/
├── .github/workflows/test.yml  # CI configuration
├── cmd/
│   ├── shfmt/                 # Main formatter tool
│   │   ├── main.go            # Entry point for shfmt
│   │   ├── shfmt.1.scd        # Man page (markdown-like)
│   │   └── Dockerfile         # Multi-stage Docker build
│   └── gosh/                  # Proof-of-concept interpreter
│       └── main.go
├── syntax/                    # Parser and AST (core package)
│   ├── parser.go              # Main parser logic
│   ├── printer.go             # Code formatting/printing
│   ├── lexer.go               # Tokenization
│   ├── nodes.go               # AST node definitions
│   ├── walk.go                # AST traversal
│   ├── simplify.go            # Code simplification
│   └── typedjson/             # JSON serialization of AST
├── interp/                    # Shell interpreter
│   ├── api.go                 # Public API
│   ├── runner.go              # Execution engine
│   ├── builtin.go             # Built-in commands
│   ├── vars.go                # Variable handling
│   └── handler.go             # Customization points
├── expand/                    # Shell expansion (variables, globs, etc.)
│   ├── expand.go              # Main expansion logic
│   ├── param.go               # Parameter expansion
│   ├── environ.go             # Environment variables
│   └── braces.go              # Brace expansion
├── pattern/                   # Pattern matching (globs)
│   └── pattern.go
├── shell/                     # High-level shell utilities
│   └── expand.go
├── fileutil/                  # File detection utilities
│   └── file.go
├── moreinterp/                # Extended interpreter (SEPARATE MODULE)
│   ├── go.mod                 # Independent go.mod file
│   └── coreutils/             # Unix-like utilities
├── go.mod                     # Main module dependencies
└── README.md
```

### Key Files by Function

**Parsing & Formatting:**
- `syntax/parser.go` - Main parser (POSIX/Bash/mksh dialects)
- `syntax/printer.go` - Code formatter
- `syntax/nodes.go` - AST definitions (all shell constructs)
- `syntax/tokens.go` - Token definitions

**Interpretation:**
- `interp/api.go` - Public interpreter API
- `interp/runner.go` - Command execution engine
- `interp/builtin.go` - Built-in shell commands (cd, echo, etc.)

**Main Commands:**
- `cmd/shfmt/main.go` - CLI formatter (~600 lines)
- `cmd/gosh/main.go` - Interactive shell demo

### Platform-Specific Files
- `expand/expand_windows.go` - Windows-specific path handling
- `interp/os_unix.go` - Unix-specific functionality (fork, signals)
- `interp/os_notunix.go` - Non-Unix platforms

## CI/CD and Validation Pipeline

### GitHub Actions Workflow (.github/workflows/test.yml)

**Matrix Testing:**
- Go versions: 1.24.x, 1.25.x
- Platforms: ubuntu-latest, macos-latest, windows-latest
- Timeout: 10 minutes per job

**Test Stages (in order):**
1. `go test ./...` - Main test suite
2. `cd moreinterp && go test ./...` - Separate module tests
3. `go test -race ./...` - Race detection (Linux only)
4. `GOARCH=386 go test -count=1 ./...` - 32-bit compatibility (Linux only)
5. Bash 5.2 confirmation tests using dockexec (Linux only)
6. Cross-compilation tests: `GOOS=plan9 GOARCH=amd64 go build ./...` and `GOOS=js GOARCH=wasm go build ./...` (Linux + Go 1.25.x only)

**Static Checks (Linux + Go 1.25.x only):**
1. `diff <(echo -n) <(gofmt -s -d .)` - Must produce no output
2. `go vet ./...` - Must pass

**Docker Build:**
- Tests multi-platform Docker images
- Platforms: linux/386, linux/amd64, linux/arm/v7, linux/arm64/v8, linux/ppc64le
- Builds both standard and Alpine variants

### Pre-Commit Checklist

Before committing changes, ALWAYS run these commands in order:

```bash
# 1. Format check
gofmt -s -d .
# Expected: No output

# 2. Go vet
go vet ./...
# Expected: No errors

# 3. Main tests
go test ./...
# Expected: All pass in ~25-30 seconds

# 4. Moreinterp tests
cd moreinterp && go test ./... && cd ..
# Expected: All pass in ~7-10 seconds

# 5. Race tests (if changing concurrency)
go test -race ./...
# Expected: All pass in ~40-50 seconds
```

### Validation Philosophy

**Testing Strategy:**
- Test files are named `*_test.go` and live alongside source files
- Heavy use of table-driven tests
- Integration tests in `interp/interp_test.go` (~3000+ test cases)
- Parser tests in `syntax/parser_test.go` and `syntax/filetests_test.go`
- Fuzzing support via Go's native fuzzing (`FuzzParsePrint`, `FuzzQuote`)

**What to Test:**
- Shell parsing correctness across dialects (POSIX, Bash, mksh)
- Formatting idempotency (parse → print → parse should be identical)
- Interpreter behavior matching real shells
- Error handling and edge cases

## Important Caveats and Known Issues

### Parser Limitations
1. **Associative array indexing**: Always use quotes: `${array["key"]}` not `${array[key]}`
2. **Arithmetic ambiguity**: `$((foo); (bar))` is not supported; use `$( (foo); (bar))` with space
3. **Keywords**: `export`, `let`, `declare` are parsed as keywords, not regular commands
4. **Brace expansion**: `declare {a,b}=c` is not supported in the parser

### Interpreter Limitations
1. **Pure Go constraints**: No real forking (uses goroutines), no direct PID/file descriptor manipulation
2. **Subshells**: Use goroutines instead of actual process forks
3. **Signal handling**: Limited compared to native shells due to Go runtime

### Development Notes
- **TODO comments**: Many exist for v4 API changes (search for `TODO(v4)`)
- **Cross-compilation**: Code supports plan9, js/wasm, and embedded systems
- **No external build tools**: Pure Go, no Makefiles or build scripts
- **Module structure**: `moreinterp` is intentionally separate to avoid heavy dependencies in main module

## Making Changes

### Code Style
- Follow standard Go formatting (`gofmt -s`)
- Use table-driven tests
- Keep packages focused (syntax, interp, expand are distinct concerns)
- Minimal comments unless explaining non-obvious shell semantics
- Use TODO comments for future improvements: `// TODO(v4): description`

### Testing New Features
1. Add test cases to existing `*_test.go` files
2. For parser changes: update `syntax/parser_test.go` or `syntax/filetests_test.go`
3. For interpreter changes: update `interp/interp_test.go`
4. Run full test suite: `go test ./... && cd moreinterp && go test ./...`
5. Consider adding fuzz tests for parsing/formatting changes

### Common Change Patterns

**Adding a new shell syntax feature:**
1. Update lexer (`syntax/lexer.go`) if new tokens needed
2. Update parser (`syntax/parser.go` or `syntax/parser_arithm.go`)
3. Update AST nodes (`syntax/nodes.go`) if new node type needed
4. Update printer (`syntax/printer.go`) for formatting
5. Add tests in `syntax/parser_test.go`

**Adding a new built-in command:**
1. Add function in `interp/builtin.go`
2. Register in `Runner` setup
3. Add comprehensive tests in `interp/interp_test.go`

**Fixing a formatting bug:**
1. Identify issue in `syntax/printer.go`
2. Add test case showing current vs expected output
3. Fix printer logic
4. Verify with `go test ./syntax`

## Quick Reference

### File Extensions and Languages
- `.sh` - Implies POSIX (unless overridden by shebang)
- `.bash` - Bash syntax
- `.mksh` - MirBSD Korn shell syntax
- `.bats` - Bash Automated Testing System

### Important Environment Variables
- `CGO_ENABLED=0` - Used in Docker builds and some CI tests
- `GOARCH=386` - Used for 32-bit testing
- `GOOS=plan9/js/...` - Used for cross-compilation testing

### EditorConfig Support
shfmt supports EditorConfig files for formatting options. Parser/printer flags override EditorConfig settings.

---

**Trust these instructions.** Only search for additional information if something here is incomplete, incorrect, or if you encounter unexpected behavior. The commands and sequences documented here have been validated against the actual repository and CI pipeline.

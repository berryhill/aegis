# Production Go CLI and Daemon Practices for Aegis

Research performed on 2026-07-17 using current upstream and standard-library sources fetched directly with `curl`.

## Executive summary

For Aegis, the strongest production design is:

1. Keep `main` extremely small and call a testable `run` function.
2. Construct a fresh Cobra command tree through constructors; do not use package-level `rootCmd`, flags, Viper, loggers, or service singletons.
3. Treat Viper as a configuration-input adapter, not as the configuration API used throughout the application.
4. Resolve all configuration once into a typed immutable `Config`, validate it, and inject that value into services.
5. Establish the process context in `main` with `signal.NotifyContext`, pass it through `ExecuteContext`, and use `cmd.Context()` in command handlers.
6. Run daemon components under coordinated cancellation, normally with `errgroup.WithContext`.
7. Return errors to one process boundary that prints exactly once and selects an exit code. Call `os.Exit` only after all cleanup-capable functions have returned.
8. Inject explicit dependencies—logger, clock, filesystem, network clients, service factories, stdin/stdout/stderr—rather than retrieving them from global state.
9. Build a new command tree for every test and test commands in-process using controlled arguments, context, and I/O.
10. Expose build provenance through Cobra’s `Version` field, with linker-injected release values and `runtime/debug.ReadBuildInfo` as a development-build fallback.
11. Let a service manager supervise the daemon. Do not self-fork, create PID files, or implement restart loops inside Aegis.

The upstream Cobra guide still demonstrates globals and `init()` registration, but that is an introductory convenience pattern, not the architecture recommended here for a testable production application.

---

## 1. Research scope and current versions

This document began as architecture-level research before the implementation existed. Aegis now has a Go module, a constructor-built Cobra CLI, strict Viper configuration, shared application services, and executable tests; current behavior is documented in the repository `README.md` and source tree.

### Sourced facts

As of the research date:

| Component | Current release | Relevant detail |
|---|---:|---|
| Go | `go1.26.5` | Released 2026-07-07; `go1.25.12` is also supported |
| Cobra | `v1.10.2` | Released 2025-12-04 |
| Viper | `v1.21.0` | Released 2025-09-08 |
| pflag | `v1.0.10` | Released 2025-09-02 |
| go-viper/mapstructure | `v2.5.0` | Released 2026-01-12 |
| `golang.org/x/sync` | `v0.22.0` | Released 2026-07-01 |

Viper `v1.21.0` declares Go 1.23 as its minimum language/module version and depends on pflag `v1.0.10` and mapstructure `v2.4.0`.

Go’s release policy supports each major release until two newer major releases exist. On the research date, the supported Go lines are therefore 1.25 and 1.26.

Sources:

- Go downloads: https://go.dev/dl/
- Go release policy/history: https://go.dev/doc/devel/release
- Cobra `v1.10.2`: https://github.com/spf13/cobra/releases/tag/v1.10.2
- Cobra release metadata: https://api.github.com/repos/spf13/cobra/releases/latest
- Viper `v1.21.0`: https://github.com/spf13/viper/releases/tag/v1.21.0
- Viper release metadata: https://api.github.com/repos/spf13/viper/releases/latest
- Viper `v1.21.0` module file: https://github.com/spf13/viper/blob/v1.21.0/go.mod
- pflag `v1.0.10`: https://github.com/spf13/pflag/releases/tag/v1.0.10
- mapstructure `v2.5.0`: https://github.com/go-viper/mapstructure/releases/tag/v2.5.0
- `x/sync` module metadata: https://proxy.golang.org/golang.org/x/sync/@latest

### Recommendation

Use the latest patched supported Go toolchain for release builds—currently Go 1.26.5. If Aegis intentionally supports the previous Go release, use a Go 1.25 language baseline and test both 1.25 and 1.26 in CI. Do not retain a Go 1.23 or 1.24 baseline merely because Viper can compile there; those Go lines are no longer receiving security fixes.

Pin Cobra and Viper deliberately in `go.mod`, and review release notes before updating rather than automatically accepting untested dependency changes.

---

## 2. Package and command layout

### Sourced facts

The Go project’s module-layout guidance says:

- Larger commands may put supporting packages under `internal`.
- `internal` prevents packages outside the parent module tree from importing those implementation packages.
- Repositories with multiple commands conventionally place each executable under `cmd/<name>`.
- Server projects generally keep implementation packages under `internal` and executable entry points under `cmd`.

Source:

- https://go.dev/doc/modules/layout

Cobra’s guide commonly shows a bare `main.go` and command files under `cmd/`. It also permits subcommands to live in separate packages.

Source:

- https://github.com/spf13/cobra/blob/main/site/content/user_guide.md

### Recommendation

If Aegis produces both a CLI and a daemon binary, use separate entry points but share application services:

```text
aegis/
├── cmd/
│   ├── aegis/
│   │   └── main.go
│   └── aegisd/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── cli.go
│   │   ├── daemon.go
│   │   └── deps.go
│   ├── command/
│   │   ├── root.go
│   │   ├── daemon.go
│   │   ├── status.go
│   │   └── version.go        # only if richer than --version
│   ├── config/
│   │   ├── config.go
│   │   ├── load.go
│   │   └── validate.go
│   ├── daemon/
│   │   ├── service.go
│   │   └── lifecycle.go
│   ├── logging/
│   │   └── logging.go
│   └── version/
│       └── version.go
├── go.mod
└── go.sum
```

Possible alternatives:

- If `aegis daemon` is the only daemon interface, one `cmd/aegis` binary may be enough.
- If packaging or privilege separation needs a distinct service binary, retain both `aegis` and `aegisd`.
- Put only genuinely reusable public APIs outside `internal`. Do not create public packages merely to achieve internal separation.

Avoid a single large `cmd` package containing business logic. Command packages should translate CLI input into calls on application services.

---

## 3. Command construction and avoiding global state

### Sourced facts

Cobra commands support:

- `RunE`, `PreRunE`, `PersistentPreRunE`, and corresponding non-error-returning variants.
- `SetArgs` for overriding process arguments, especially in tests.
- `SetIn`, `SetOut`, and `SetErr` for controlled I/O.
- `ExecuteContext` and command contexts.
- `SilenceErrors` and `SilenceUsage`.
- Positional argument validators and required/mutually-exclusive flag groups.

Source:

- https://github.com/spf13/cobra/blob/main/command.go
- https://github.com/spf13/cobra/blob/main/args.go
- https://github.com/spf13/cobra/blob/main/site/content/user_guide.md

Viper explicitly says that its package-level global singleton is generally discouraged because it makes testing harder and may cause unexpected behavior. Viper recommends creating an instance and passing it where necessary; the global instance may be deprecated in the future.

Source:

- https://github.com/spf13/viper/blob/master/README.md#should-viper-be-a-global-singleton-or-passed-around

### Recommendation

Build the entire command tree with constructors:

```go
type Dependencies struct {
    Logger  *slog.Logger
    Stdout  io.Writer
    Stderr  io.Writer
    Stdin   io.Reader
    NewAPI  func(Config) (API, error)
    Version version.Info
}

func NewRootCommand(deps Dependencies) *cobra.Command {
    opts := rootOptions{}

    root := &cobra.Command{
        Use:           "aegis",
        Short:         "Aegis security service",
        Version:       deps.Version.String(),
        SilenceErrors: true,
        SilenceUsage:  true,
    }

    root.SetIn(deps.Stdin)
    root.SetOut(deps.Stdout)
    root.SetErr(deps.Stderr)

    root.PersistentFlags().StringVar(
        &opts.configFile,
        "config",
        "",
        "configuration file",
    )

    root.AddCommand(
        newDaemonCommand(deps, &opts),
        newStatusCommand(deps, &opts),
    )

    return root
}
```

Key rules:

- No package-level `rootCmd`.
- No package-level flag destination variables.
- No command registration in `init()`.
- No package-level `viper.Get*` calls.
- No `slog.SetDefault` requirement for application operation.
- No package-level mutable `Config`.
- Construct the command tree once per actual invocation and once per test case.
- Pass only the dependencies each command needs when practical; a small top-level dependency bundle is acceptable, but avoid turning it into a service locator.

Although Cobra’s introductory guide uses globals and `init()`, explicit constructors make initialization order visible and permit isolated tests and multiple command instances in one process.

---

## 4. Context cancellation and daemon lifecycle

### Sourced facts

The Go `context` package specifies that:

- Context should be propagated across API boundaries.
- A context should generally be the first function parameter.
- Contexts should not be stored in structs as a substitute for explicit method parameters.
- Context values are for request-scoped data, not optional parameters or general dependency injection.
- Returned cancellation functions must be called to release resources.
- The same context may be used concurrently by multiple goroutines.

Source:

- https://pkg.go.dev/context
- https://github.com/golang/go/blob/master/src/context/context.go

`signal.NotifyContext` returns a context canceled by a listed signal, parent cancellation, or the returned stop function. Its stop function unregisters signal handling and releases associated resources.

Source:

- https://pkg.go.dev/os/signal#NotifyContext
- https://github.com/golang/go/blob/master/src/os/signal/signal.go

Cobra’s `ExecuteContext` stores the context on commands, and command handlers can retrieve it with `cmd.Context()`.

Source:

- https://pkg.go.dev/github.com/spf13/cobra#Command.ExecuteContext
- https://github.com/spf13/cobra/blob/main/command.go

`errgroup.WithContext` provides goroutine synchronization, first-error propagation, and derived-context cancellation. The derived context is canceled when the first task returns an error or when `Wait` returns.

Source:

- https://pkg.go.dev/golang.org/x/sync/errgroup
- https://github.com/golang/sync/blob/master/errgroup/errgroup.go

`http.Server.Shutdown`:

- Closes listeners.
- Closes idle connections.
- Waits for active connections to become idle.
- Returns if its shutdown context expires.
- Does not close or wait for hijacked connections such as WebSockets.
- Causes serving methods to return `http.ErrServerClosed`.

Source:

- https://pkg.go.dev/net/http#Server.Shutdown
- https://github.com/golang/go/blob/master/src/net/http/server.go

### Recommendation

Create process cancellation at the outer boundary and pass it to Cobra:

```go
func main() {
    os.Exit(run())
}

func run() int {
    ctx, stop := signal.NotifyContext(
        context.Background(),
        os.Interrupt,
        syscall.SIGTERM,
    )
    defer stop()

    deps, err := buildDependencies()
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        return 1
    }

    cmd := NewRootCommand(deps)
    if err := cmd.ExecuteContext(ctx); err != nil {
        return reportError(os.Stderr, err)
    }
    return 0
}
```

Every long-running command should use the Cobra context:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    cfg, err := loadAndValidateConfig(cmd.Context(), opts)
    if err != nil {
        return fmt.Errorf("load configuration: %w", err)
    }

    return deps.Daemon.Run(cmd.Context(), cfg)
},
```

For a daemon with multiple cooperating components:

```go
func (d *Daemon) Run(ctx context.Context, cfg Config) error {
    group, ctx := errgroup.WithContext(ctx)

    group.Go(func() error {
        return d.api.Serve(ctx, cfg.API)
    })
    group.Go(func() error {
        return d.worker.Run(ctx, cfg.Worker)
    })
    group.Go(func() error {
        return d.metrics.Serve(ctx, cfg.Metrics)
    })

    return group.Wait()
}
```

Additional lifecycle recommendations:

- Every goroutine started for the lifetime of the daemon must have an owner and a termination condition.
- Every blocking operation should either accept a context or have an explicit close/shutdown mechanism.
- Give graceful shutdown a bounded timeout.
- Keep the process alive until graceful shutdown finishes; do not return from the root command immediately after starting shutdown.
- Treat `http.ErrServerClosed` as expected only when shutdown was intentionally initiated.
- Explicitly track WebSocket, hijacked, streaming, or protocol-upgraded connections because `http.Server.Shutdown` does not wait for them.
- Consider a second termination signal an operator request for immediate termination. If implemented, make this behavior explicit and test it.
- Do not use `context.WithoutCancel` for ordinary background workers; doing so silently detaches work from shutdown.
- Do not store the process context inside a general application dependency container. Pass it to `Run`, `Serve`, and operation methods.

---

## 5. Configuration precedence and Viper integration

### Sourced facts

Viper documents the following descending precedence:

1. Explicit `Set`
2. Flags
3. Environment variables
4. Configuration file
5. External key/value stores
6. Defaults

Additional Viper behavior:

- Keys are case-insensitive.
- Environment variables are case-sensitive.
- Empty environment variables are considered unset unless `AllowEmptyEnv` is enabled.
- Environment values are read when accessed and are not cached.
- A single Viper instance supports one configuration file, although multiple search paths may be configured.
- Binding a flag does not copy its value immediately; the value is resolved when accessed.
- `Get*` methods return a type’s zero value if a key is absent unless the caller checks `IsSet`.
- Viper does not deep-merge complex overridden values; an overridden complex value is replaced.
- Viper supports unmarshalling into typed structs using `mapstructure` tags.
- Concurrent reads and writes to a Viper instance are not safe without external synchronization.
- The package-level singleton is discouraged.
- Viper v2 remains under discussion; current v1 compatibility and stability are prioritized.

Source:

- https://github.com/spf13/viper/blob/master/README.md

### Recommendation

Publish and enforce an Aegis-specific precedence contract:

```text
command-line flags
> environment variables
> explicit or discovered configuration file
> compiled defaults
```

Avoid Viper `Set` for routine application overrides because it silently creates a precedence level above flags. Reserve it for tests or deliberately documented internal overrides.

Use an instance:

```go
func Load(path string, flags *pflag.FlagSet) (Config, error) {
    v := viper.New()

    v.SetEnvPrefix("AEGIS")
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
    v.AutomaticEnv()

    setDefaults(v)

    if err := v.BindPFlags(flags); err != nil {
        return Config{}, fmt.Errorf("bind flags: %w", err)
    }

    if path != "" {
        v.SetConfigFile(path)
    } else {
        v.SetConfigName("aegis")
        v.SetConfigType("yaml")
        // Add only documented search paths in a documented order.
    }

    if err := readConfig(v, path != ""); err != nil {
        return Config{}, err
    }

    var cfg Config
    if err := v.UnmarshalExact(&cfg); err != nil {
        return Config{}, fmt.Errorf("decode configuration: %w", err)
    }
    if err := cfg.Validate(); err != nil {
        return Config{}, fmt.Errorf("invalid configuration: %w", err)
    }

    return cfg, nil
}
```

The resulting `Config` should be a typed value passed to constructors. Application packages should not depend on Viper:

```go
type Config struct {
    Log    LogConfig    `mapstructure:"log"`
    API    APIConfig    `mapstructure:"api"`
    Worker WorkerConfig `mapstructure:"worker"`
}
```

Recommended behavior:

- If `--config` names a file explicitly, absence is fatal.
- If config discovery is optional and no file is found, continue with env/flags/defaults.
- If a discovered file exists but cannot be read or parsed, fail startup.
- Log the selected config file at debug or info level, but do not print it unconditionally to stdout.
- Use `UnmarshalExact` or equivalent strict decoding to reject unknown keys.
- Validate after all sources have been merged.
- Document environment mappings such as `api.listen-address` → `AEGIS_API_LISTEN_ADDRESS`.
- Bind only known keys; do not use unconstrained environment ingestion as an undocumented configuration namespace.
- Keep secrets out of diagnostics and “effective config” output.
- If exposing a config-printing command, redact credential, token, key, certificate, and private endpoint fields.

### Important Cobra/Viper trap

The Cobra guide notes that binding a flag to Viper does not mean a Go variable originally bound to that flag will receive a value sourced from the config file or environment. Reading the pflag destination variable directly can therefore bypass Viper’s merged result.

Source:

- https://github.com/spf13/cobra/blob/main/site/content/user_guide.md#bind-flags-with-config

Recommendation: after parsing, decode Viper into `Config` and use `Config` exclusively. Do not sometimes read flag variables and sometimes call `v.Get*`.

---

## 6. Validation

### Sourced facts

Cobra includes:

- Positional argument validators such as `NoArgs`, `ExactArgs`, `RangeArgs`, and `MatchAll`.
- Required flags.
- Mutually exclusive flag groups.
- Groups whose members must appear together.
- Groups requiring at least one flag.

Source:

- https://github.com/spf13/cobra/blob/main/site/content/user_guide.md
- https://github.com/spf13/cobra/blob/main/args.go

Viper unmarshalling uses mapstructure semantics and `mapstructure` tags by default.

Source:

- https://github.com/spf13/viper/blob/master/README.md#unmarshaling
- https://github.com/spf13/viper/blob/master/TROUBLESHOOTING.md

### Recommendation

Use three validation layers:

1. **CLI shape validation**
   - Positional argument count.
   - Required or mutually exclusive flags.
   - Simple enum parsing where the flag type can enforce it.

2. **Configuration schema validation**
   - Unknown fields.
   - String-to-duration, URL, CIDR, and enum decoding.
   - Numeric ranges.
   - Required fields after precedence resolution.

3. **Semantic/runtime validation**
   - Cross-field rules.
   - Mutually dependent subsystems.
   - Filesystem permissions.
   - TLS certificate/key pairing.
   - Listen-address conflicts.
   - Features requiring credentials or external services.

Keep validation pure where possible:

```go
func (c Config) Validate() error {
    var errs []error

    if c.API.ListenAddress == "" {
        errs = append(errs, errors.New("api.listen-address is required"))
    }
    if c.API.ReadTimeout <= 0 {
        errs = append(errs, errors.New("api.read-timeout must be positive"))
    }
    if c.TLS.CertFile == "" && c.TLS.KeyFile != "" {
        errs = append(errs, errors.New(
            "tls.cert-file and tls.key-file must be configured together",
        ))
    }

    return errors.Join(errs...)
}
```

Do not make ordinary validation failures panic. Report all independent configuration errors together where possible so operators can fix a file in one pass.

Avoid performing network calls or starting goroutines in Cobra argument validators. Such work belongs after configuration resolution and should use the command context.

---

## 7. Logging

### Sourced facts

The standard `log/slog` package provides structured logging with:

- Severity levels.
- Key/value attributes.
- Text and line-delimited JSON handlers.
- Context-aware methods such as `InfoContext` and `ErrorContext`.
- Logger derivation with `With` and `WithGroup`.
- Dynamic level control via `slog.LevelVar`.
- `LogValuer`, which can control representation and redact sensitive fields.

The documentation recommends passing a context to an output method when available.

Source:

- https://pkg.go.dev/log/slog
- https://github.com/golang/go/blob/master/src/log/slog/doc.go

### Recommendation

Use an injected `*slog.Logger`, not package-level logging calls:

```go
type Service struct {
    log *slog.Logger
}

func NewService(log *slog.Logger) *Service {
    return &Service{log: log.With("component", "service")}
}
```

Policy:

- Daemon logs go to stderr or the platform logging sink.
- Command results intended for users or pipes go to stdout.
- Human diagnostics go to stderr.
- JSON command output must never be mixed with logs on stdout.
- Select text vs. JSON at startup; JSON is generally preferable under service supervision.
- Include stable fields such as `component`, `operation`, `request_id`, and `duration`.
- Do not log the same error at every layer. Add context while returning it; log once at the layer that owns handling or discarding it.
- Redact secrets by construction. Avoid logging whole configuration structs, request headers, tokens, or private keys.
- Use `ErrorContext(ctx, ...)` and other context-aware calls where request or trace metadata can be extracted.
- Avoid global `slog.SetDefault` unless adapting an external package that only uses default logging. The application should still retain and inject its explicit logger.
- If log-level reload is required, `slog.LevelVar` is concurrency-safe and avoids rebuilding the whole logger.

For a CLI, `--verbose` should control diagnostic verbosity, not change normal command output. If supporting repeatable `-v`, document its exact mapping and cap it rather than allowing accidental unbounded levels.

---

## 8. Errors, diagnostics, and exit codes

### Sourced facts

Cobra’s `RunE` and related `*E` hooks return errors to the caller. Cobra can either print errors itself or suppress them through `SilenceErrors`; it can suppress automatic usage output through `SilenceUsage`.

Source:

- https://github.com/spf13/cobra/blob/main/command.go
- https://github.com/spf13/cobra/blob/main/site/content/user_guide.md#returning-and-handling-errors

Go’s `os.Exit` documentation states:

- Zero conventionally means success.
- Nonzero conventionally means error.
- Deferred functions are not run.
- Portable status values should be in the range 0–125.

Source:

- https://pkg.go.dev/os#Exit
- https://github.com/golang/go/blob/master/src/os/proc.go

The standard `flag` package uses exit status 2 for command-line parse errors under `ExitOnError`.

Source:

- https://pkg.go.dev/flag#ErrorHandling
- https://github.com/golang/go/blob/master/src/flag/flag.go

### Recommendation

Centralize exit selection:

```go
type ExitCoder interface {
    ExitCode() int
}

func reportError(w io.Writer, err error) int {
    if err == nil {
        return 0
    }

    fmt.Fprintln(w, "aegis:", err)

    var ec ExitCoder
    if errors.As(err, &ec) {
        return ec.ExitCode()
    }
    return 1
}
```

Suggested stable contract:

| Code | Meaning |
|---:|---|
| `0` | Success |
| `1` | Runtime or operational failure |
| `2` | Invalid command usage, flags, arguments, or configuration supplied by the user |
| Other `3–125` codes | Only if Aegis has a documented automation requirement |

Avoid returning 130 or 143 through `os.Exit` if strict portability to Go’s documented 0–125 range matters. If shell-style signal status is important to Aegis automation, document it as a platform convention and test it separately.

Use this control flow:

```go
func main() {
    os.Exit(run())
}
```

All resources acquired by `run` or below can then be cleaned up by deferred calls before `run` returns. Do not call `os.Exit`, `log.Fatal`, or `cobra.CheckErr` in library or command-handler code because they bypass normal cleanup and make in-process tests difficult.

Error policy:

- Wrap errors with operation context using `%w`.
- Match expected classes with `errors.Is` and `errors.As`, not string inspection.
- Keep machine-readable error categories separate from human prose.
- Print each terminal error once.
- Show usage for syntax/usage errors, not for daemon crashes, connection failures, permission errors, or cancellation.
- Treat expected cancellation during graceful shutdown differently from failure. An operator-requested clean shutdown should normally return success after cleanup.
- Include actionable nouns and operations: `open config "/etc/aegis.yaml": permission denied`, not `startup failed`.

Setting both `SilenceErrors` and `SilenceUsage` on the root command is usually the cleanest approach when Aegis owns centralized rendering. A typed usage error can explicitly request usage output.

---

## 9. Dependency injection

### Sourced fact

Go’s context documentation explicitly says context values should carry request-scoped values across APIs, not optional function parameters.

Source:

- https://pkg.go.dev/context

### Recommendation

Prefer constructor injection:

```go
type Clock interface {
    Now() time.Time
}

type Repository interface {
    Load(ctx context.Context, id string) (Record, error)
}

type Controller struct {
    log   *slog.Logger
    clock Clock
    repo  Repository
}

func NewController(
    log *slog.Logger,
    clock Clock,
    repo Repository,
) *Controller {
    return &Controller{
        log:   log,
        clock: clock,
        repo:  repo,
    }
}
```

Inject at natural boundaries:

- `*slog.Logger`
- Typed immutable config
- Repository/client interfaces
- Clock or timer abstraction only where deterministic timing matters
- `io.Reader`/`io.Writer`
- Filesystem abstraction only where file operations are significant
- Factories for expensive or command-specific clients
- Build/version information

Do not:

- Put all dependencies in `context.Context`.
- Create a global service registry.
- Define interfaces in advance “for testability” when only one implementation and no boundary exists.
- Inject a huge `Dependencies` object deep into all packages.
- Construct network clients in package `init`.
- Let commands reach into a global application singleton.

Interfaces should normally be defined by the consuming package and remain narrow. Concrete types are preferable until substitution is actually useful.

---

## 10. Testing strategy

### Sourced facts

Cobra exposes controlled argument and I/O methods:

- `SetArgs`
- `SetIn`
- `SetOut`
- `SetErr`
- `ExecuteContext`

Source:

- https://github.com/spf13/cobra/blob/main/command.go

Go’s standard tooling supports:

- Unit and example tests through `testing`.
- Fuzzing.
- The race detector.
- `go vet`.
- Machine-readable `go test -json`.

The Go security guidance recommends fuzzing, `go test -race`, `go vet`, dependency updates, and `govulncheck`.

Sources:

- https://pkg.go.dev/testing
- https://go.dev/doc/security/best-practices
- https://go.dev/doc/tutorial/govulncheck

### Recommendation

### Command tests

Construct a fresh command per test:

```go
func executeCommand(
    t *testing.T,
    ctx context.Context,
    deps Dependencies,
    args ...string,
) (stdout string, stderr string, err error) {
    t.Helper()

    var out bytes.Buffer
    var errOut bytes.Buffer

    deps.Stdout = &out
    deps.Stderr = &errOut
    deps.Stdin = strings.NewReader("")

    cmd := NewRootCommand(deps)
    cmd.SetArgs(args)

    err = cmd.ExecuteContext(ctx)
    return out.String(), errOut.String(), err
}
```

Test:

- Help and version output.
- Unknown command and flag behavior.
- Positional argument validation.
- Every precedence pair: flag over env, env over file, file over default.
- Explicit missing config vs. optional discovery.
- Unknown config keys.
- Invalid durations, addresses, enums, and cross-field combinations.
- stdout/stderr separation.
- Exact exit classification without spawning a process.
- Context cancellation.
- Repeated execution by creating a new command tree, not reusing a mutated one.

### Configuration tests

Use table-driven tests and `t.Setenv`. Test config decoding independently of Cobra. Include fixtures for every supported format if Aegis promises multiple formats.

Fuzz:

- Config file decoders.
- Address, selector, policy, and expression parsers.
- Inputs crossing privilege or trust boundaries.
- Any custom mapstructure decode hooks.

### Service tests

- Inject fakes or in-memory implementations.
- Verify shutdown waits for owned goroutines.
- Verify blocked operations unblock on cancellation.
- Verify partial startup failure tears down already-started components.
- Use real loopback listeners for a small number of integration tests rather than mocking all `net` behavior.
- Use bounded test deadlines to detect leaked shutdown paths.

### Process-level tests

Use subprocess tests only for behavior that cannot be verified in-process:

- Actual exit status.
- OS signal handling.
- Service-manager integration.
- stdout/stderr file descriptors.
- Build/version metadata.
- Privilege and filesystem behavior.

### CI baseline

At minimum:

```sh
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

Also run formatting checks, static analysis appropriate to the project, and tests on all supported Go lines and operating systems. Do not assume `-race` covers unexecuted code paths; the official documentation explicitly notes that it detects only races occurring at runtime.

---

## 11. Version embedding and provenance

### Sourced facts

If Cobra’s root command has a non-empty `Version` field, Cobra adds a top-level `--version` flag. Its format can be customized with `SetVersionTemplate`.

Source:

- https://github.com/spf13/cobra/blob/main/site/content/user_guide.md#version-flag
- https://github.com/spf13/cobra/blob/main/command.go

Go embeds module and VCS build information in binaries under appropriate build conditions. `runtime/debug.ReadBuildInfo` exposes the main module, dependencies, and build settings. VCS settings can include revision, time, and dirty status.

Source:

- https://pkg.go.dev/runtime/debug#ReadBuildInfo
- https://github.com/golang/go/blob/master/src/runtime/debug/mod.go
- https://go.dev/doc/go1.18#debug/buildinfo

### Recommendation

Represent version data explicitly:

```go
package version

var (
    Version = "dev"
    Commit  = ""
    Date    = ""
)

type Info struct {
    Version string
    Commit  string
    Date    string
    Dirty   bool
}
```

Inject release values with linker flags:

```sh
go build \
  -trimpath \
  -ldflags "\
    -X 'example.org/aegis/internal/version.Version=${VERSION}' \
    -X 'example.org/aegis/internal/version.Commit=${COMMIT}' \
    -X 'example.org/aegis/internal/version.Date=${BUILD_DATE}'" \
  ./cmd/aegis
```

Use `debug.ReadBuildInfo` as a fallback for local and `go install` builds, not as an excuse to make release metadata nondeterministic.

Recommended output:

```text
aegis 1.8.0
commit: 0123456789abcdef
built: 2026-07-17T18:00:00Z
go: go1.26.5
platform: linux/amd64
```

Guidelines:

- `aegis --version` should be fast and must not load configuration or initialize network clients.
- Use stdout for successful version output.
- Prefer semantic release versions.
- Include commit identity and dirty status in verbose version output.
- Use a reproducible timestamp source such as `SOURCE_DATE_EPOCH` when reproducibility is a requirement.
- Do not rely only on a manually edited constant.
- Add a release test that builds the binary and executes `--version`.

---

## 12. Daemon supervision and deployment

### Sourced facts

The systemd service documentation says PID files should be avoided in modern projects. It recommends `Type=notify`, `Type=notify-reload`, or `Type=simple` where possible, avoiding needless forking. It also documents bounded stop behavior through `TimeoutStopSec`; when a service fails to stop within that interval, it can be forcibly terminated.

Source:

- https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html

### Recommendation

Aegis should run in the foreground and let systemd, Kubernetes, launchd, Windows Service Control Manager, or another supervisor manage:

- Process lifetime.
- Restart policy.
- Log collection.
- User/group identity.
- Resource limits.
- Readiness ordering.
- Stop timeout.
- Environment and secret injection.

Do not implement:

- Double-fork daemonization.
- PID-file ownership.
- Internal crash-restart loops.
- Log-file rotation when the deployment environment already provides it.
- Arbitrary backgrounding from the main daemon command.

For systemd, prefer `Type=exec` or `Type=simple` unless Aegis implements `sd_notify` correctly. If readiness matters to dependent services, consider `Type=notify` with explicit readiness notification only after listeners are established and mandatory initialization has succeeded.

Align application and supervisor timeouts:

```text
Aegis graceful shutdown timeout < systemd TimeoutStopSec
```

This leaves time for Aegis to report a shutdown timeout and exit before the supervisor sends `SIGKILL`.

A health endpoint should distinguish:

- **Liveness:** process is functioning and not irrecoverably stuck.
- **Readiness:** process can currently serve useful requests.
- **Startup:** optional gate for unusually slow initialization.

On shutdown, set readiness false before draining traffic.

---

## 13. High-risk traps

### Cobra traps

1. **Package-level commands and `init()` registration**
   - Hidden mutation and initialization ordering.
   - Difficult to construct multiple independent trees.
   - Leaks state between tests.

2. **Reusing one command tree across tests**
   - Cobra and pflag retain parsed and command state.
   - Build a fresh tree each time.

3. **Using `Run` where failure is possible**
   - Encourages printing or exiting inside handlers.
   - Prefer `RunE`.

4. **Heavy work in `PersistentPreRunE`**
   - Makes inheritance subtle and can initialize dependencies for commands that do not need them.
   - Keep common pre-run work narrow; load command-specific dependencies in the command path.

5. **Persistent hook assumptions**
   - Cobra’s persistent-hook traversal has nuanced inheritance behavior, and global `EnableTraverseRunHooks` changes it.
   - Avoid lifecycle correctness that depends on complex parent-hook chains.

6. **Printing usage for all errors**
   - A database outage is not a syntax mistake.
   - Silence automatic usage and classify errors centrally.

7. **Using `cobra.CheckErr`**
   - It is convenient but centralizes behavior inside Cobra and can terminate execution.
   - Return errors to Aegis’s process boundary instead.

8. **Writing directly to `os.Stdout`**
   - Bypasses command I/O injection.
   - Use `cmd.OutOrStdout()` or an injected writer.

### Viper traps

1. **Using the global singleton**
   - Explicitly discouraged upstream.
   - Causes cross-test and cross-command state leakage.

2. **Reading bound flag variables after Viper merging**
   - Config/env values do not populate those variables.
   - Decode once into typed config.

3. **Assuming deep merge**
   - Viper replaces complex overridden values.
   - Test nested-object override behavior explicitly.

4. **Assuming absent keys are distinguishable from zero**
   - `Get*` returns zero values.
   - Use typed decoding, pointer/optional fields where necessary, or `IsSet`.

5. **Unknown-key acceptance**
   - Typographical errors can silently disable intended settings.
   - Use strict unmarshalling.

6. **Case and separator mismatches**
   - Viper keys are case-insensitive, env vars are not, and nested keys require an explicit replacer.
   - Publish one canonical key mapping.

7. **Environment emptiness**
   - Empty env values default to “unset.”
   - Decide whether empty is a valid explicit value and configure accordingly.

8. **Live config reload**
   - Viper is not safe for concurrent reads and writes.
   - A file-change callback can expose partial or invalid state if configuration is consumed directly from Viper.

   Prefer immutable startup configuration. If reload is required:
   - Read into a new Viper instance or raw buffer.
   - Decode a new typed config.
   - Validate it completely.
   - Apply only explicitly reloadable fields.
   - Atomically swap a complete snapshot.
   - Retain the old valid config when reload fails.
   - Synchronize callbacks and shutdown.

9. **Multiple config files**
   - A Viper instance natively reads one file, though it can search multiple paths.
   - If Aegis supports layering multiple files, define and implement that merge explicitly rather than implying Viper does it automatically.

10. **Config write APIs in a daemon**
    - Writing merged config may serialize secrets or destroy user comments and formatting.
    - Keep runtime configuration loading read-only unless Aegis explicitly provides a safe config-management command.

### General Go traps

1. **Calling `os.Exit` below `main`**
   - Deferred cleanup will not run.

2. **Using context as a dependency bag**
   - Violates the context package’s intended use and hides dependencies.

3. **Starting unowned goroutines**
   - Leads to leaks and incomplete shutdown.

4. **Treating cancellation as an error everywhere**
   - Clean operator shutdown should not generate alarming error logs or restart loops.

5. **Logging and returning the same error repeatedly**
   - Produces duplicate records without adding information.

6. **Self-daemonizing**
   - Interferes with modern supervision and readiness tracking.

7. **Unbounded shutdown**
   - A stuck client or worker prevents deployment and recovery.

8. **No network timeouts**
   - Context cancellation alone does not compensate for every poorly configured server/client timeout.

9. **Logging secrets via structured values**
   - Structured logging can serialize more fields than expected. Use explicit safe attributes or `LogValuer`.

10. **Version initialization that loads the application**
    - `--version` and completion should not require a valid config, filesystem permissions, database, or network.

---

## 14. Recommended Aegis initialization sequence

A production startup path should be explicit and linear:

```text
main
  └─ establish signal-aware root context
  └─ establish stdin/stdout/stderr
  └─ derive build information
  └─ construct lightweight root command
      └─ Cobra parses command and flags
      └─ selected RunE:
          └─ create a new Viper instance
          └─ set defaults
          └─ bind selected command flags
          └─ bind/document environment
          └─ read optional or explicit config file
          └─ strict-decode typed Config
          └─ validate Config
          └─ create logger from validated logging config
          └─ construct command-specific services
          └─ run operation with cmd.Context()
          └─ gracefully stop and close resources
  └─ classify returned error
  └─ print once
  └─ return exit code
main calls os.Exit after run has returned
```

This sequence deliberately keeps command construction side-effect-free. Help, shell completion, and version operations remain fast and do not require daemon dependencies.

---

## 15. Prioritized implementation checklist

### Highest priority

- [ ] Replace global Cobra commands with `NewRootCommand`.
- [ ] Replace package-level Viper calls with `viper.New()`.
- [ ] Decode configuration once into a typed immutable `Config`.
- [ ] Add strict unknown-field and semantic validation.
- [ ] Pass a signal-aware context through `ExecuteContext`.
- [ ] Ensure every daemon goroutine exits on context cancellation.
- [ ] Centralize error rendering and exit-code selection.
- [ ] Ensure only `main` calls `os.Exit`.
- [ ] Separate stdout command results from stderr diagnostics/logs.
- [ ] Add bounded graceful shutdown.

### Next priority

- [ ] Inject loggers and command I/O.
- [ ] Add fresh-command-per-test helpers.
- [ ] Test complete configuration precedence.
- [ ] Add cancellation and partial-startup-failure tests.
- [ ] Add `--version` with release and VCS provenance.
- [ ] Run `go test -race`, `go vet`, and `govulncheck` in CI.
- [ ] Document config keys, env mappings, precedence, and exit codes.
- [ ] Provide service-manager deployment units without PID files or self-forking.

### If live reload is required

- [ ] Enumerate reloadable vs. startup-only fields.
- [ ] Parse and validate a complete replacement snapshot.
- [ ] Apply snapshots atomically.
- [ ] Retain the last valid snapshot on failure.
- [ ] Synchronize reload with shutdown.
- [ ] Test rapid changes, invalid files, deleted files, and concurrent reads.

---

## Primary source index

### Go

- Module layout: https://go.dev/doc/modules/layout
- Current downloads: https://go.dev/dl/
- Release policy/history: https://go.dev/doc/devel/release
- Context documentation: https://pkg.go.dev/context
- Context source: https://github.com/golang/go/blob/master/src/context/context.go
- Signal contexts: https://pkg.go.dev/os/signal#NotifyContext
- Signal source: https://github.com/golang/go/blob/master/src/os/signal/signal.go
- HTTP graceful shutdown: https://pkg.go.dev/net/http#Server.Shutdown
- HTTP server source: https://github.com/golang/go/blob/master/src/net/http/server.go
- Structured logging: https://pkg.go.dev/log/slog
- slog package documentation source: https://github.com/golang/go/blob/master/src/log/slog/doc.go
- Process exit: https://pkg.go.dev/os#Exit
- `os.Exit` source: https://github.com/golang/go/blob/master/src/os/proc.go
- Standard flag error handling: https://pkg.go.dev/flag#ErrorHandling
- Build information: https://pkg.go.dev/runtime/debug#ReadBuildInfo
- Build-info source: https://github.com/golang/go/blob/master/src/runtime/debug/mod.go
- Go security practices: https://go.dev/doc/security/best-practices
- Testing package: https://pkg.go.dev/testing

### Cobra, Viper, and related projects

- Cobra repository: https://github.com/spf13/cobra
- Cobra user guide: https://github.com/spf13/cobra/blob/main/site/content/user_guide.md
- Cobra command API source: https://github.com/spf13/cobra/blob/main/command.go
- Cobra argument validation source: https://github.com/spf13/cobra/blob/main/args.go
- Cobra `v1.10.2`: https://github.com/spf13/cobra/releases/tag/v1.10.2
- Viper repository and documentation: https://github.com/spf13/viper
- Viper README: https://github.com/spf13/viper/blob/master/README.md
- Viper source: https://github.com/spf13/viper/blob/master/viper.go
- Viper troubleshooting: https://github.com/spf13/viper/blob/master/TROUBLESHOOTING.md
- Viper `v1.21.0`: https://github.com/spf13/viper/releases/tag/v1.21.0
- pflag: https://github.com/spf13/pflag
- mapstructure: https://github.com/go-viper/mapstructure
- errgroup: https://pkg.go.dev/golang.org/x/sync/errgroup
- errgroup source: https://github.com/golang/sync/blob/master/errgroup/errgroup.go

### Service supervision

- systemd service documentation: https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html

The behavioral statements under “Sourced facts” above come directly from these upstream sources. The architecture, policy, package-layout choices, exit-code mapping, and Aegis-specific implementation guidance are recommendations derived from those facts.

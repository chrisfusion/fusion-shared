# Logging Principles — fusion-platform Go/Gin Services

These principles apply to every Go service in the fusion-platform that uses Gin.
Follow them exactly so all services produce consistent, correlatable structured logs.

---

## Library choice

Use **`log/slog`** from the Go standard library (Go 1.21+). Do not import `logrus`, `zerolog`,
or `zap` directly. `slog` is the call-site interface for all services; the backend handler can
be swapped in one place in `main.go` without touching any logging call site.

If a service already uses `controller-runtime` (operator binaries), leave its `logr`/`zap`
setup untouched — operator and server can coexist with different internal loggers.

---

## Configuration

Two env vars control logging, both wired through Helm into the server's ConfigMap:

| Env var | Values | Default | Purpose |
|---|---|---|---|
| `LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` | `info` | Minimum level to emit |
| `LOG_FORMAT` | `json` \| `text` | `json` | Output format |

JSON is always the default — Kubernetes log collectors (Loki, ELK, Datadog) parse it
natively. Use `text` only in local dev environments.

Add both to the service's `Config` struct and `Load()` function:

```go
// internal/config/config.go
type Config struct {
    // ... existing fields ...
    LogLevel  string // "debug" | "info" | "warn" | "error"
    LogFormat string // "json" | "text"
}

func Load() *Config {
    return &Config{
        // ... existing fields ...
        LogLevel:  getEnv("LOG_LEVEL", "info"),
        LogFormat: getEnv("LOG_FORMAT", "json"),
    }
}
```

---

## Helm integration

Three files must be touched to expose `LOG_LEVEL` and `LOG_FORMAT` as configurable Helm values.

### 1. deployment/values.yaml

Add under `server.config` (the same block as `authEnabled`, `indexBackendURL`, etc.):

```yaml
server:
  config:
    # ... existing fields ...
    # Log level: debug | info | warn | error
    logLevel: "info"
    # Log format: json | text
    logFormat: "json"
```

### 2. deployment/templates/server-configmap.yaml

Add two entries to the ConfigMap data block:

```yaml
data:
  # ... existing entries ...
  LOG_LEVEL: {{ .Values.server.config.logLevel | quote }}
  LOG_FORMAT: {{ .Values.server.config.logFormat | quote }}
```

### 3. deployment/templates/server-deployment.yaml

The ConfigMap must be mounted into the pod via `envFrom`. If `envFrom` is already present
(most fusion-platform services have it), no change is needed — the new keys are picked up
automatically. If it is absent, add it to the container spec:

```yaml
containers:
  - name: server
    # ... image, ports, etc. ...
    envFrom:
      - configMapRef:
          name: {{ .Release.Name }}-server-config
```

With all three in place, operators can tune logging per environment without a code change:

```bash
# debug verbosity for a staging incident
helm upgrade fusion-myservice deployment/ -n fusion \
  --set server.config.logLevel=debug

# human-readable output for local dev
helm upgrade fusion-myservice deployment/ -n fusion \
  --set server.config.logFormat=text
```

---

## Logger setup in main.go

Call `setupLogger(cfg)` as the **first thing** in `main()`, before any other log call.
This ensures even startup errors are emitted in the correct format.

```go
func main() {
    cfg := config.Load()
    setupLogger(cfg)
    // ... rest of startup
}

func setupLogger(cfg *config.Config) {
    var level slog.Level
    unknownLevel := false
    switch cfg.LogLevel {
    case "debug":
        level = slog.LevelDebug
    case "warn":
        level = slog.LevelWarn
    case "error":
        level = slog.LevelError
    case "info", "":
        level = slog.LevelInfo
    default:
        level = slog.LevelInfo
        unknownLevel = true
    }

    opts := &slog.HandlerOptions{Level: level}
    var handler slog.Handler
    if cfg.LogFormat == "text" {
        handler = slog.NewTextHandler(os.Stdout, opts)
    } else {
        handler = slog.NewJSONHandler(os.Stdout, opts)
    }
    slog.SetDefault(slog.New(handler))

    if unknownLevel {
        slog.Warn("unrecognised LOG_LEVEL, defaulting to info", "value", cfg.LogLevel)
    }
}
```

---

## Gin logging middleware

Every Gin service must include a per-request logging middleware. Place it in
`internal/api/middleware/logging.go`. Wire it globally in the router **before** CORS and auth.

### middleware/logging.go

```go
package middleware

import (
    "crypto/rand"
    "encoding/hex"
    "log/slog"
    "time"

    "github.com/gin-gonic/gin"
)

const loggerKey = "slog_logger"

// NewLoggingMiddleware generates a request ID, stamps a per-request *slog.Logger
// with {request_id, method, path, client_ip}, stores it in gin.Context, and logs
// the access line (status + latency) after the handler returns.
func NewLoggingMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()

        b := make([]byte, 8)
        _, _ = rand.Read(b)
        reqID := hex.EncodeToString(b)

        path := c.FullPath()
        if path == "" {
            path = c.Request.URL.Path // fallback for unmatched routes (404)
        }

        logger := slog.Default().With(
            "request_id", reqID,
            "method", c.Request.Method,
            "path", path,
            "client_ip", c.ClientIP(),
        )
        c.Set(loggerKey, logger)

        c.Next()

        logger.Info("request",
            "status", c.Writer.Status(),
            "latency_ms", time.Since(start).Milliseconds(),
        )
    }
}

// LoggerFromCtx returns the per-request logger set by NewLoggingMiddleware.
// Falls back to slog.Default() if the middleware was not applied.
func LoggerFromCtx(c *gin.Context) *slog.Logger {
    v, exists := c.Get(loggerKey)
    if !exists {
        return slog.Default()
    }
    if logger, ok := v.(*slog.Logger); ok {
        return logger
    }
    return slog.Default()
}
```

### Router registration

Always use `gin.New()` (not `gin.Default()` — it adds its own logger). Register the
logging middleware immediately after `gin.Recovery()`:

```go
r := gin.New()
r.Use(gin.Recovery())
r.Use(middleware.NewLoggingMiddleware()) // must come before CORS and auth
r.Use(corsMiddleware())
// ... health routes, api group, auth middleware
```

---

## Handler logging rules

### 1. Always use LoggerFromCtx — never use slog directly inside handlers

```go
// correct
middleware.LoggerFromCtx(c).Error("find artifact", "name", fullName, "error", err)

// wrong — loses request_id and other per-request fields
slog.Error("find artifact", "name", fullName, "error", err)
```

### 2. internalError must log

The shared `internalError` helper is the catch-all for unexpected 500 errors (DB errors,
K8s client errors, etc.). It must log before writing the HTTP response:

```go
func internalError(c *gin.Context, err error) {
    middleware.LoggerFromCtx(c).Error("internal error", "error", err)
    c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
```

### 3. Log with context fields, not format strings

Prefer key-value structured fields over a formatted string message:

```go
// correct
middleware.LoggerFromCtx(c).Error("create version in registry",
    "name", req.Name,
    "version", req.Version,
    "error", err)

// wrong — unstructured, can't be queried
log.Printf("forge: create version %s/%s in registry: %v", req.Name, req.Version, err)
```

### 4. Don't double-log

When a handler logs explicitly with context fields and then writes the HTTP response
directly, do **not** also call `internalError` — that would log the same error twice.

```go
// correct — log once with context, write response directly
if err != nil {
    middleware.LoggerFromCtx(c).Error("find artifact", "name", fullName, "error", err)
    c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
    return
}

// also correct — no extra context needed, delegate to internalError
if err != nil {
    internalError(c, err)
    return
}

// wrong — logs twice
if err != nil {
    middleware.LoggerFromCtx(c).Error("find artifact", "name", fullName, "error", err)
    internalError(c, err) // logs again
    return
}
```

Use the first form when you have extra key-value context worth capturing (artifact name,
build ID, version, etc.). Use `internalError` alone for simple cases where the error
message is sufficient.

### 5. Non-fatal errors use Warn, not Error

Status sync failures, degraded-mode fallbacks, and other recoverable conditions:

```go
middleware.LoggerFromCtx(c).Warn("sync status failed", "build_id", id, "error", err)
```

---

## Startup logging

Use `slog.Info`/`slog.Error` (global, since no request context exists yet) for the startup
sequence. Fatal conditions use `slog.Error` then `os.Exit(1)` — slog has no `Fatalf`.

```go
// startup progress
slog.Info("database connected")
slog.Info("loaded validation rules", "exact_pinning", rules.RequireExactPinning)
slog.Info("starting server", "addr", addr)

// fatal startup failure
if err := pool.Ping(ctx); err != nil {
    slog.Error("ping database", "error", err)
    os.Exit(1)
}
```

Do **not** use `log.Fatal`, `log.Printf`, or `fmt.Println` anywhere in the codebase.

---

## Package-level (background) logging

For code that runs outside a request context (background goroutines, workers, startup
helpers), use the global `slog` functions directly:

```go
slog.Warn("cannot read rules file, using defaults", "path", path, "error", err)
```

---

## What the access log looks like

Every request produces exactly one access log line at INFO, emitted by the middleware
after the handler returns. Example JSON output:

```json
{"time":"2026-05-19T10:00:00.123Z","level":"INFO","msg":"request","request_id":"a1b2c3d4e5f6a7b8","method":"POST","path":"/api/v1/venvs","client_ip":"10.0.0.5","status":202,"latency_ms":48}
```

Error lines from the same request carry the same `request_id`, making correlation trivial:

```json
{"time":"2026-05-19T10:00:00.100Z","level":"ERROR","msg":"create CIBuild CR","request_id":"a1b2c3d4e5f6a7b8","method":"POST","path":"/api/v1/venvs","client_ip":"10.0.0.5","name":"forge-venv-42","error":"namespaces \"fusion\" not found"}
```

---

## Future: switching to a zap backend

All call sites (`slog.Info`, `middleware.LoggerFromCtx(c).Error`, etc.) are stable across
backend changes. To switch to a zap-backed slog handler (e.g. for unified pipeline with a
`controller-runtime` operator), only `setupLogger` in `main.go` needs to change:

```go
// example — swap handler, zero call-site changes
import slogzap "github.com/samber/slog-zap/v2"

zapLogger, _ := zap.NewProduction()
slog.SetDefault(slog.New(slogzap.Option{Logger: zapLogger}.NewZapHandler()))
```

---

## Checklist for a new service

- [ ] `LogLevel` and `LogFormat` added to `Config` struct with `getEnv()` defaults
- [ ] `setupLogger(cfg)` called first in `main()`
- [ ] No `import "log"` anywhere in the codebase
- [ ] `internal/api/middleware/logging.go` created with `NewLoggingMiddleware` + `LoggerFromCtx`
- [ ] Router uses `gin.New()`, not `gin.Default()`
- [ ] `NewLoggingMiddleware()` registered before CORS and auth middleware
- [ ] `internalError` calls `middleware.LoggerFromCtx(c).Error(...)` before writing response
- [ ] All handler error paths use `LoggerFromCtx(c)` with relevant key-value fields
- [ ] No double-logging (explicit log + `internalError` for the same error)
- [ ] `deployment/values.yaml`: `logLevel` and `logFormat` added under `server.config`
- [ ] `deployment/templates/server-configmap.yaml`: `LOG_LEVEL` and `LOG_FORMAT` entries added
- [ ] `deployment/templates/server-deployment.yaml`: container has `envFrom: - configMapRef` pointing at the server ConfigMap

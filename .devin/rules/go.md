# Go Coding Rules — Minimin Sync (Wails Desktop)

## File Size
- **Maximum 300 lines per file.** Split by domain (pkg/config, pkg/sync, pkg/instance) or by feature.

## Style & Formatting
- `gofmt` / `goimports` are mandatory.
- Import order: stdlib → third-party → project internal (`minimin-sync/pkg/...`).

## Error Handling
- Explicitly handle every error. Log errors with context via `wailsruntime.LogErrorf` before returning them to the JS layer.

## Architecture
- `App` in `app.go` is the binding surface to JS. Keep it thin: delegate business logic to `pkg/` packages.
- `pkg/sync.Service` owns sync operations (AddServer, CheckUpdates, ApplyUpdates).
- Never call `wailsruntime.EventsEmit` or `wailsruntime.Log*` from outside `App` or `pkg/sync.Service`. Keep emit points centralized.
- Exported methods on `App` are the JS API surface. Use clear names: `GetServers`, `CheckUpdates`, `RunServer`.

## Wails-Specific
- `App` methods bound to JS must have godoc comments.
- Events use kebab-case names: `addServer:progress`, `applyUpdates:done`.
- Progress and status updates go through `wailsruntime.EventsEmit`; never block the main thread.

## Testing
- Run `go test ./...` in the project root before committing.

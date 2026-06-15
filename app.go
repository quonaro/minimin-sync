package main

import (
	"context"
	"time"

	"minimin-sync/pkg/config"
	syncpkg "minimin-sync/pkg/sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx            context.Context
	config         *config.Config
	syncService    *syncpkg.Service
	autoCheckReset chan struct{}
	updateTmpPath  string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	cfg, err := config.Load()
	if err != nil {
		wailsruntime.LogErrorf(ctx, "failed to load config: %v", err)
		cfg = &config.Config{}
	}
	a.config = cfg

	if a.config.InstancesDir != "" && a.config.Launcher == "" {
		a.config.Launcher = detectLauncherFromPath(a.config.InstancesDir)
		_ = a.config.Save()
	}

	a.syncService = syncpkg.NewService(ctx, cfg.InstancesDir)
	a.autoCheckReset = make(chan struct{})
	go a.checkSelfUpdateOnStartup()
	go a.autoCheckLoop()
}

func (a *App) checkSelfUpdateOnStartup() {
	time.Sleep(3 * time.Second)
	info, err := a.CheckForUpdate()
	if err != nil {
		return
	}
	if info["available"].(bool) {
		wailsruntime.EventsEmit(a.ctx, "selfUpdate:available", info)
	}
}

// IsOperationRunning reports whether AddServer or ApplyUpdates is active.
func (a *App) IsOperationRunning() bool {
	return a.syncService.IsOperationRunning()
}

// GetVersion returns the current application version.
func (a *App) GetVersion() string {
	return version
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() config.Config {
	if a.config == nil {
		return config.Config{}
	}
	return *a.config
}

// SaveConfig persists the current configuration.
func (a *App) SaveConfig(cfg config.Config) error {
	oldInterval := a.config.AutoCheckIntervalMinutes
	a.config = &cfg
	if err := a.config.Save(); err != nil {
		return err
	}
	if a.syncService != nil {
		a.syncService = syncpkg.NewService(a.ctx, cfg.InstancesDir)
	}
	if oldInterval != cfg.AutoCheckIntervalMinutes && a.autoCheckReset != nil {
		select {
		case a.autoCheckReset <- struct{}{}:
		default:
		}
	}
	return nil
}

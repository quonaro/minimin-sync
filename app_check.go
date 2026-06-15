package main

import (
	"sync"
	"time"

	"minimin-sync/pkg/instance"
	syncpkg "minimin-sync/pkg/sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// RunManualCheck triggers an immediate auto-check across all servers.
func (a *App) RunManualCheck() {
	go func() {
		wailsruntime.EventsEmit(a.ctx, "autoCheck:start")
		a.runAutoCheck()
	}()
}

func (a *App) autoCheckLoop() {
	for {
		interval := a.config.AutoCheckIntervalMinutes
		if interval <= 0 {
			interval = 5
		}
		timer := time.NewTimer(time.Duration(interval) * time.Minute)

		if !a.syncService.IsOperationRunning() {
			wailsruntime.EventsEmit(a.ctx, "autoCheck:start")
			a.runAutoCheck()
		}

		select {
		case <-timer.C:
		case <-a.autoCheckReset:
		case <-a.ctx.Done():
			timer.Stop()
			return
		}
		timer.Stop()
	}
}

func (a *App) runAutoCheck() {
	if a.config.InstancesDir == "" {
		wailsruntime.EventsEmit(a.ctx, "autoCheck:done")
		return
	}
	servers, err := instance.Scan(a.config.InstancesDir)
	if err != nil {
		wailsruntime.EventsEmit(a.ctx, "autoCheck:done")
		return
	}

	var wg sync.WaitGroup
	for _, s := range servers {
		if s.Marker == nil {
			continue
		}
		wg.Add(1)
		go func(s instance.ScannedInstance) {
			defer wg.Done()
			res, err := a.syncService.CheckUpdates(s.Name)
			if err != nil {
				wailsruntime.EventsEmit(a.ctx, "checkUpdates:error", map[string]string{
					"serverID": s.Name,
					"error":    err.Error(),
				})
				return
			}
			wailsruntime.EventsEmit(a.ctx, "checkUpdates:ok", map[string]string{
				"serverID": s.Name,
			})
			missingCnt, outdatedCnt := 0, 0
			if v, ok := res["missing"].([]syncpkg.ManifestFile); ok {
				missingCnt = len(v)
			}
			if v, ok := res["outdated"].([]syncpkg.ManifestFile); ok {
				outdatedCnt = len(v)
			}
			wailsruntime.EventsEmit(a.ctx, "checkUpdates:result", map[string]interface{}{
				"serverID":      s.Name,
				"missingCount":  missingCnt,
				"outdatedCount": outdatedCnt,
			})
		}(s)
	}
	wg.Wait()
	wailsruntime.EventsEmit(a.ctx, "servers:reload")
	wailsruntime.EventsEmit(a.ctx, "autoCheck:done")
}

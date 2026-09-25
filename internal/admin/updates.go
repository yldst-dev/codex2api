package admin

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"codex-gateway/internal/update"
)

const (
	MetaAutoUpdate    = "auto_update"
	UpdateRequestFile = "update.request"

	UpdateModeSystemd = "systemd"
	UpdateModeSelf    = "self"

	updateInterval    = 6 * time.Hour
	updateFirstCheck  = time.Minute
	checkCooldown     = 30 * time.Second
	installTimeout    = 10 * time.Minute
	requestStaleAfter = 5 * time.Minute
)

const (
	statusIdle       = "idle"
	statusInstalling = "installing"
	statusRestarting = "restarting"
	statusRequested  = "requested"
	statusError      = "error"
)

var errUpdateBusy = errors.New("업데이트가 이미 진행 중입니다.")

type updateState struct {
	latest      *update.Release
	checkedAt   time.Time
	checkErr    string
	status      string
	err         string
	requestedAt time.Time
}

func (s *Server) updatesEnabled() bool {
	return s.Updates != nil
}

func (s *Server) autoUpdate(ctx context.Context) bool {
	v, err := s.Store.Meta(ctx, MetaAutoUpdate)
	return err == nil && v != "off"
}

func (s *Server) canUpdate() bool {
	return s.updatesEnabled() && update.ValidTag(s.Version) && (s.UpdateMode == UpdateModeSystemd || s.UpdateMode == UpdateModeSelf)
}

func (s *Server) checkUpdate(ctx context.Context, force bool) error {
	s.updMu.Lock()
	if !force && time.Since(s.upd.checkedAt) < checkCooldown {
		s.updMu.Unlock()
		return nil
	}
	s.upd.checkedAt = time.Now()
	s.updMu.Unlock()

	rel, err := s.Updates.Latest(ctx)

	s.updMu.Lock()
	defer s.updMu.Unlock()
	if err != nil {
		if errors.Is(err, update.ErrNoRelease) {
			s.upd.latest, s.upd.checkErr = nil, ""
			return nil
		}
		s.upd.checkErr = err.Error()
		return err
	}
	s.upd.latest, s.upd.checkErr = &rel, ""
	return nil
}

func (s *Server) applyUpdate() error {
	if !s.canUpdate() {
		return errors.New("이 설치 방식에서는 웹에서 업데이트할 수 없습니다. 설치 스크립트를 다시 실행해 주세요.")
	}
	s.updMu.Lock()
	s.refreshStaleLocked()
	switch s.upd.status {
	case statusInstalling, statusRestarting, statusRequested:
		s.updMu.Unlock()
		return errUpdateBusy
	}
	rel := s.upd.latest
	if rel == nil || !update.Newer(rel.Tag, s.Version) {
		s.updMu.Unlock()
		return errors.New("이미 최신 버전입니다.")
	}
	tag := rel.Tag
	if s.UpdateMode == UpdateModeSystemd {
		path := filepath.Join(s.DataDir, UpdateRequestFile)
		if err := os.WriteFile(path, []byte(tag+"\n"), 0o600); err != nil {
			s.updMu.Unlock()
			return err
		}
		s.upd.status, s.upd.err, s.upd.requestedAt = statusRequested, "", time.Now()
		s.updMu.Unlock()
		s.logger().Info("update requested", "from", s.Version, "to", tag)
		return nil
	}
	s.upd.status, s.upd.err = statusInstalling, ""
	s.updMu.Unlock()
	go s.installSelf(tag)
	return nil
}

func (s *Server) installSelf(tag string) {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	s.logger().Info("installing update", "from", s.Version, "to", tag)
	err := s.Updates.Install(ctx, tag, s.Executable)
	s.updMu.Lock()
	if err != nil {
		s.upd.status, s.upd.err = statusError, err.Error()
		s.updMu.Unlock()
		s.logger().Error("update failed", "err", err)
		return
	}
	s.upd.status = statusRestarting
	s.updMu.Unlock()
	if s.Restart != nil {
		time.AfterFunc(time.Second, s.Restart)
	}
}

func (s *Server) refreshStaleLocked() {
	if s.upd.status != statusRequested || time.Since(s.upd.requestedAt) < requestStaleAfter {
		return
	}
	s.upd.status = statusError
	s.upd.err = "업데이트 도우미가 응답하지 않았습니다. 서버에서 codex-gateway-update.path가 켜져 있는지 확인해 주세요."
	_ = os.Remove(filepath.Join(s.DataDir, UpdateRequestFile))
}

func (s *Server) updateJSON(ctx context.Context) map[string]any {
	auto := s.autoUpdate(ctx)
	s.updMu.Lock()
	defer s.updMu.Unlock()
	s.refreshStaleLocked()
	status := s.upd.status
	if status == "" {
		status = statusIdle
	}
	out := map[string]any{
		"current":     s.Version,
		"supported":   s.canUpdate(),
		"mode":        s.UpdateMode,
		"auto":        auto,
		"status":      status,
		"error":       s.upd.err,
		"check_error": s.upd.checkErr,
		"available":   false,
		"latest":      nil,
	}
	if !s.upd.checkedAt.IsZero() {
		out["checked_at"] = s.upd.checkedAt.UTC().Format(time.RFC3339)
	}
	if rel := s.upd.latest; rel != nil {
		out["latest"] = map[string]any{
			"tag":          rel.Tag,
			"url":          rel.URL,
			"published_at": rel.PublishedAt.UTC().Format(time.RFC3339),
		}
		out["available"] = update.Newer(rel.Tag, s.Version)
	}
	return out
}

func (s *Server) updateLoop(ctx context.Context) {
	timer := time.NewTimer(updateFirstCheck)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if err := s.checkUpdate(ctx, true); err != nil {
			s.logger().Warn("update check failed", "err", err)
		} else if s.autoUpdate(ctx) && s.canUpdate() {
			s.updMu.Lock()
			available := s.upd.latest != nil && update.Newer(s.upd.latest.Tag, s.Version)
			s.updMu.Unlock()
			if available {
				if err := s.applyUpdate(); err != nil && !errors.Is(err, errUpdateBusy) {
					s.logger().Warn("automatic update failed", "err", err)
				}
			}
		}
		timer.Reset(updateInterval)
	}
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if !s.updatesEnabled() {
		writeError(w, http.StatusConflict, "업데이트 확인이 꺼져 있습니다.")
		return
	}
	if err := s.checkUpdate(r.Context(), false); err != nil {
		writeError(w, http.StatusBadGateway, "새 버전을 확인하지 못했습니다: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"update": s.updateJSON(r.Context())})
}

func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if err := s.applyUpdate(); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errUpdateBusy) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"update": s.updateJSON(r.Context())})
}

func (s *Server) updateAuto(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	value := "on"
	if !body.Enabled {
		value = "off"
	}
	if err := s.Store.SetMeta(r.Context(), MetaAutoUpdate, value); err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"update": s.updateJSON(r.Context())})
}

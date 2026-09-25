package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

type Runner struct {
	Handler http.Handler
	Log     *slog.Logger

	mu   sync.Mutex
	srv  *http.Server
	addr string
}

func (r *Runner) Addr() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addr
}

func (r *Runner) Start(addr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.srv != nil && addr == r.addr {
		return nil
	}
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		if r.srv != nil {
			go r.stop(r.srv, 30*time.Second)
		}
		r.serve(ln, addr)
		return nil
	}
	if r.srv == nil || !samePort(addr, r.addr) {
		return err
	}
	old := r.addr
	r.stop(r.srv, 5*time.Second)
	r.srv, r.addr = nil, ""
	ln, err = net.Listen("tcp", addr)
	if err == nil {
		r.serve(ln, addr)
		return nil
	}
	back, backErr := net.Listen("tcp", old)
	if backErr != nil {
		return fmt.Errorf("%w; restoring %s also failed: %v", err, old, backErr)
	}
	r.serve(back, old)
	return err
}

func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	srv := r.srv
	r.srv, r.addr = nil, ""
	r.mu.Unlock()
	if srv == nil {
		return nil
	}
	if err := srv.Shutdown(ctx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		r.logger().Warn("closing connections still open after shutdown timeout")
		_ = srv.Close()
	}
	return nil
}

func (r *Runner) logger() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

func (r *Runner) serve(ln net.Listener, addr string) {
	log := r.logger()
	srv := &http.Server{
		Handler:           r.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	r.srv, r.addr = srv, addr
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("gateway listener stopped", "addr", addr, "err", err)
		}
	}()
}

func (r *Runner) stop(srv *http.Server, grace time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
}

func samePort(a, b string) bool {
	_, pa, errA := net.SplitHostPort(a)
	_, pb, errB := net.SplitHostPort(b)
	return errA == nil && errB == nil && pa == pb
}

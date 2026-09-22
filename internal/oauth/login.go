package oauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

type CallbackResult struct {
	Token *Token
	Err   error
}

func (f *Flow) WaitCallback(ctx context.Context) (*Token, error) {
	ln, err := net.Listen("tcp", CallbackAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", CallbackAddr, err)
	}
	result := make(chan CallbackResult, 1)
	var accepted atomic.Bool
	send := func(res CallbackResult) {
		select {
		case result <- res:
		default:
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if !accepted.CompareAndSwap(false, true) {
			http.Error(w, "Login already finished.", http.StatusConflict)
			return
		}
		tok, err := f.ExchangeCallback(r.Context(), r.URL.RequestURI())
		if err != nil {
			http.Error(w, "Login failed. Return to the terminal.", http.StatusBadRequest)
			send(CallbackResult{Err: err})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("<!doctype html><html><body><p>Login complete. You can close this window.</p></body></html>"))
		send(CallbackResult{Token: tok})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not found.", http.StatusNotFound)
	})
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			send(CallbackResult{Err: err})
		}
	}()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-result:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Token, nil
	}
}

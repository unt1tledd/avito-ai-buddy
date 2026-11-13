package controller_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"avito-ai-buddy/auth/controller"
)

func TestHealth(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
		body string
	}{
		{"ready", nil, http.StatusOK, `{"status":"ok"}`},
		{"database unavailable", errors.New("database unavailable"), http.StatusServiceUnavailable, `{"status":"unavailable"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			controller.RegisterHealth(mux, func(ctx context.Context) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Error("database check has no deadline")
				}
				return tc.err
			})
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
			if recorder.Code != tc.code || recorder.Body.String() != tc.body {
				t.Fatalf("health = (%d, %q), want (%d, %q)", recorder.Code, recorder.Body.String(), tc.code, tc.body)
			}
		})
	}
}

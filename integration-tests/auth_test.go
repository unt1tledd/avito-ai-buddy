//go:build integration

package integration_test

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"sync"
	"testing"
)

func TestAuthLifecycle(t *testing.T) {
	s := setup(t)
	id, token, email := s.account(t)
	r := s.call(t, "POST", s.auth+"/v1/auth/validate", token, nil, 200)
	var valid struct {
		Valid bool  `json:"is_valid"`
		ID    int64 `json:"user_id"`
	}
	decode(t, r.body, &valid)
	require.True(t, valid.Valid)
	require.Equal(t, id, valid.ID)
	s.call(t, "POST", s.auth+"/v1/auth/register", "", credentials(email), 409)
	s.call(t, "POST", s.auth+"/v1/auth/login", "", map[string]string{"email": email, "password": "wrong-password"}, 401)
	login := s.call(t, "POST", s.auth+"/v1/auth/login", "", credentials(email), 200)
	var refresh *http.Cookie
	for _, c := range login.cookies {
		require.True(t, c.HttpOnly)
		if c.Name == "refresh_token" && c.MaxAge > 0 {
			refresh = c
		}
	}
	require.NotNil(t, refresh)
	require.Equal(t, "/v1/auth", refresh.Path)
	s.call(t, "POST", s.auth+"/v1/auth/validate", refresh.Value, nil, 401)
	s.call(t, "POST", s.auth+"/v1/auth/refresh", "", nil, 401, &http.Cookie{Name: "refresh_token", Value: token})
	renewed := s.call(t, "POST", s.auth+"/v1/auth/refresh", "", nil, 200, refresh)
	s.call(t, "POST", s.auth+"/v1/auth/refresh", "", nil, 401, refresh)
	var result struct {
		Token string `json:"access_token"`
	}
	decode(t, renewed.body, &result)
	require.NotEmpty(t, result.Token)
	r = s.call(t, "POST", s.auth+"/v1/auth/validate", result.Token, nil, 200)
	decode(t, r.body, &valid)
	require.Equal(t, id, valid.ID)

	var newRefresh *http.Cookie
	for _, c := range renewed.cookies {
		if c.Name == "refresh_token" && c.MaxAge > 0 {
			newRefresh = c
		}
	}
	require.NotNil(t, newRefresh)
	s.call(t, "POST", s.auth+"/v1/auth/logout", "", nil, 204, newRefresh)
	s.call(t, "POST", s.auth+"/v1/auth/refresh", "", nil, 401, newRefresh)
}

func TestAuthInvalidInput(t *testing.T) {
	s := setup(t)
	for _, tc := range []struct {
		name, path string
		body       any
		want       int
	}{
		{"bad email", "register", map[string]string{"email": "invalid", "password": password}, 400},
		{"short password", "register", map[string]string{"email": "short@example.test", "password": "123"}, 400},
		{"empty login", "login", map[string]string{}, 400},
		{"invalid token", "validate", map[string]string{"token": "not-a-jwt"}, 401},
		{"missing refresh", "refresh", nil, 401},
	} {
		t.Run(tc.name, func(t *testing.T) { s.call(t, "POST", s.auth+"/v1/auth/"+tc.path, "", tc.body, tc.want) })
	}
}

func TestConcurrentRegistrationCreatesOneAccount(t *testing.T) {
	s := setup(t)
	_, _, base := s.account(t)
	email := "race-" + base
	const n = 4
	results := make([]response, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = s.request(t.Context(), "POST", s.auth+"/v1/auth/register", "", credentials(email))
		}()
	}
	close(start)
	wg.Wait()
	created := 0
	for i, r := range results {
		require.NoError(t, errs[i])
		switch r.status {
		case 201:
			created++
		case 409:
		default:
			t.Errorf("unexpected registration status %d", r.status)
		}
	}
	require.Equal(t, 1, created)
	r := s.call(t, "POST", s.auth+"/v1/auth/login", "", credentials(email), 200)
	require.Contains(t, string(r.body), "access_token", fmt.Sprint(created))
}

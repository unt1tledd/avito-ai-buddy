package plan_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	"avito-ai-buddy/onboarding/internal/repository/plan"
	"avito-ai-buddy/pkg/transactor"
)

func TestAssignPlan(t *testing.T) {
	const lockQuery = "-- name: LockTrackTemplate "
	const assignQuery = "-- name: AssignPlan "
	lockFields := []uint32{pgtype.Int8OID}
	planFields := []uint32{pgtype.Int8OID, pgtype.Int8OID, pgtype.TextOID}
	begin := commandReply("begin", 'T')
	commit := commandReply("commit", 'I')
	rollback := commandReply("rollback", 'I')
	locked := selectReply(lockQuery, lockFields, "1")
	assigned := selectReply(assignQuery, planFields, "7", "42", "qa")

	for _, tc := range []struct {
		name     string
		replies  []planDBReply
		want     entity.Plan
		wantErr  error
		wantCode string
	}{
		{name: "assign and commit", replies: []planDBReply{begin, locked, assigned, commit}, want: entity.Plan{ID: 7, UserID: 42, Track: "qa"}},
		{name: "unsupported track rolls back", replies: []planDBReply{begin, selectReply(lockQuery, lockFields), rollback}, wantErr: onboardingerrors.ErrUnsupportedTrack},
		{name: "track conflict rolls back", replies: []planDBReply{begin, locked, selectReply(assignQuery, planFields), rollback}, wantErr: onboardingerrors.ErrPlanTrackConflict},
		{name: "lock error rolls back", replies: []planDBReply{begin, errorReply(lockQuery, 'E'), rollback}, wantCode: "40001"},
		{name: "assignment error rolls back", replies: []planDBReply{begin, locked, errorReply(assignQuery, 'E'), rollback}, wantCode: "40001"},
		{name: "commit error returns no plan", replies: []planDBReply{begin, locked, assigned, errorReply("commit", 'I')}, wantCode: "40001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newAssignmentPool(t, tc.replies)
			repo := plan.NewPlanRepository(nil, transactor.NewTransactor(pool))
			got, err := repo.AssignPlan(t.Context(), 42, "qa")
			if tc.wantCode != "" {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != tc.wantCode {
					t.Fatalf("AssignPlan error = %v, want PostgreSQL code %s", err, tc.wantCode)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AssignPlan error = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("AssignPlan = %+v, want %+v", got, tc.want)
			}
		})
	}
}

type planDBReply struct {
	queryPrefix string
	txStatus    byte
	messages    []pgproto3.BackendMessage
}

func commandReply(command string, status byte) planDBReply {
	return planDBReply{command, status, []pgproto3.BackendMessage{
		&pgproto3.CommandComplete{CommandTag: []byte(strings.ToUpper(command))},
	}}
}

func errorReply(query string, status byte) planDBReply {
	return planDBReply{query, status, []pgproto3.BackendMessage{
		&pgproto3.ErrorResponse{Severity: "ERROR", Code: "40001", Message: "serialization failure"},
	}}
}

func selectReply(query string, typeOIDs []uint32, values ...string) planDBReply {
	description := &pgproto3.RowDescription{Fields: make([]pgproto3.FieldDescription, len(typeOIDs))}
	for i, oid := range typeOIDs {
		description.Fields[i].DataTypeOID = oid
	}
	messages := []pgproto3.BackendMessage{description}
	if len(values) > 0 {
		row := &pgproto3.DataRow{Values: make([][]byte, len(values))}
		for i, value := range values {
			row.Values[i] = []byte(value)
		}
		messages = append(messages, row)
	}
	messages = append(messages, &pgproto3.CommandComplete{CommandTag: []byte("SELECT")})
	return planDBReply{query, 'T', messages}
}

// newAssignmentPool uses the real pgx pool and transactor with scripted replies
// over an in-memory connection. The SQL itself is exercised by the PostgreSQL
// integration test.
func newAssignmentPool(t *testing.T, replies []planDBReply) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig("host=127.0.0.1 user=test dbname=test sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	config.MaxConns = 1
	client, server := net.Pipe()
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		_ = server.SetDeadline(time.Now().Add(5 * time.Second))
		done <- serveAssignmentQueries(server, replies)
	}()
	t.Cleanup(func() {
		_ = client.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return client, nil
	}

	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func serveAssignmentQueries(conn net.Conn, replies []planDBReply) error {
	server := pgproto3.NewBackend(conn, conn)
	if _, err := server.ReceiveStartupMessage(); err != nil {
		return fmt.Errorf("receive startup: %w", err)
	}
	server.Send(&pgproto3.AuthenticationOk{})
	server.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	server.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
	server.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := server.Flush(); err != nil {
		return err
	}

	for _, reply := range replies {
		message, err := server.Receive()
		if err != nil {
			return fmt.Errorf("waiting for %q: %w", reply.queryPrefix, err)
		}
		query, ok := message.(*pgproto3.Query)
		if !ok || !strings.HasPrefix(query.String, reply.queryPrefix) {
			return fmt.Errorf("expected query %q, got %v", reply.queryPrefix, message)
		}
		for _, response := range reply.messages {
			server.Send(response)
		}
		server.Send(&pgproto3.ReadyForQuery{TxStatus: reply.txStatus})
		if err := server.Flush(); err != nil {
			return err
		}
	}
	return nil
}

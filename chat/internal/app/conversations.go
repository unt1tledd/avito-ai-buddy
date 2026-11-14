package app

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Conversation routes authenticate every operation and scope all SQL to its owner.
// Deletion is soft: in-flight actions and audit records keep their references.
func registerConversationRoutes(mux *http.ServeMux, db *pgxpool.Pool, sessions sessionService) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fail := func(code int, msg string) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": msg})
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			if c, e := r.Cookie("access_token"); e == nil {
				token = c.Value
			}
		}
		owner, _, err := sessions.GetSession(r.Context(), token)
		if err != nil {
			fail(401, "Требуется вход")
			return
		}
		userID, err := strconv.ParseInt(owner, 10, 64)
		if err != nil {
			fail(401, "Требуется вход")
			return
		}
		type item struct {
			ID        string    `json:"id"`
			Title     string    `json:"title"`
			CreatedAt time.Time `json:"createdAt"`
		}
		if r.Method == "GET" {
			rows, err := db.Query(r.Context(), `SELECT c.id,COALESCE((SELECT left(body,80) FROM chat.message WHERE conversation_id=c.id AND role='user' ORDER BY id LIMIT 1),c.title),c.created_at FROM chat.conversation c WHERE user_id=$1 AND deleted_at IS NULL AND (NOT is_default OR EXISTS(SELECT 1 FROM chat.message WHERE conversation_id=c.id)) ORDER BY created_at DESC,id DESC`, userID)
			if err != nil {
				fail(500, "Не удалось загрузить чаты")
				return
			}
			defer rows.Close()
			list := []item{}
			for rows.Next() {
				var v item
				if err := rows.Scan(&v.ID, &v.Title, &v.CreatedAt); err != nil {
					fail(500, "Не удалось загрузить чаты")
					return
				}
				list = append(list, v)
			}
			if rows.Err() != nil {
				fail(500, "Не удалось загрузить чаты")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"conversations": list})
			return
		}
		if r.Method == "POST" {
			var v item
			err := db.QueryRow(r.Context(), `INSERT INTO chat.conversation(user_id,is_default) VALUES($1,false) RETURNING id,title,created_at`, userID).Scan(&v.ID, &v.Title, &v.CreatedAt)
			if err != nil {
				fail(500, "Не удалось создать чат")
				return
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(v)
			return
		}
		if r.Method == "DELETE" {
			query := `UPDATE chat.conversation SET deleted_at=now() WHERE user_id=$1 AND deleted_at IS NULL`
			args := []any{userID}
			if id := r.PathValue("id"); id != "" {
				n, e := strconv.ParseInt(id, 10, 64)
				if e != nil || n <= 0 {
					fail(400, "Некорректный чат")
					return
				}
				query += " AND id=$2"
				args = append(args, n)
			}
			result, e := db.Exec(r.Context(), query, args...)
			if e != nil {
				fail(500, "Не удалось удалить чат")
				return
			}
			if len(args) == 2 && result.RowsAffected() == 0 {
				fail(404, "Чат не найден")
				return
			}
			w.WriteHeader(204)
		}
	}
	mux.HandleFunc("GET /v1/chat/conversations", handler)
	mux.HandleFunc("POST /v1/chat/conversations", handler)
	mux.HandleFunc("DELETE /v1/chat/conversations", handler)
	mux.HandleFunc("DELETE /v1/chat/conversations/{id}", handler)
}

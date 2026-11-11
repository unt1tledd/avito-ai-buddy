-- name: InsertProfile :execrows
INSERT INTO users.profiles (user_id, last_name, first_name, middle_name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO NOTHING;

-- name: GetProfile :one
SELECT user_id, last_name, first_name, middle_name
FROM users.profiles
WHERE user_id = $1;

-- name: EnsureTeamMembership :exec
INSERT INTO users.team_members (team_id, user_id, role)
VALUES ($1, $2, 'member')
ON CONFLICT (team_id, user_id) DO NOTHING;

-- name: CreateInternship :one
INSERT INTO users.internships (
    intern_user_id, team_id, mentor_user_id, track, start_date, planned_end_date, actual_end_date
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (intern_user_id, team_id, start_date) DO NOTHING
RETURNING internship_id;

-- name: GetMetadata :one
SELECT
    i.internship_id, i.intern_user_id, i.team_id, i.mentor_user_id, i.track,
    i.start_date, i.planned_end_date, i.actual_end_date,
    i.planned_end_date - i.start_date AS planned_duration_days,
    COALESCE(i.actual_end_date - i.start_date, 0)::int4 AS actual_duration_days,
    p.last_name, p.first_name, p.middle_name, t.name AS team_name,
    mentor.last_name AS mentor_last_name, mentor.first_name AS mentor_first_name,
    mentor.middle_name AS mentor_middle_name
FROM users.internships AS i
JOIN users.profiles AS p ON p.user_id = i.intern_user_id
JOIN users.teams AS t ON t.team_id = i.team_id
LEFT JOIN users.profiles AS mentor ON mentor.user_id = i.mentor_user_id
WHERE i.intern_user_id = $1
ORDER BY i.start_date DESC, i.internship_id DESC
LIMIT 1;

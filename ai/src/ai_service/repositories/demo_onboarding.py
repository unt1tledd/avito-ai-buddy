import sqlite3
from contextlib import closing
from pathlib import Path


def save_completion(path: Path, user_id: str, step_id: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with closing(sqlite3.connect(path)) as connection, connection:
        connection.execute(
            "CREATE TABLE IF NOT EXISTS completed_steps ("
            "user_id TEXT NOT NULL, step_id TEXT NOT NULL, PRIMARY KEY (user_id, step_id))"
        )
        connection.execute(
            "INSERT INTO completed_steps (user_id, step_id) VALUES (?, ?) "
            "ON CONFLICT (user_id, step_id) DO NOTHING",
            (user_id, step_id),
        )

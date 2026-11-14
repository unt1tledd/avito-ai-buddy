import argparse
from datetime import date, timedelta
import html
import json
import math
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import time
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
DOCKER = (["sudo"] if os.getenv("DOCKER_USE_SUDO") == "1" else []) + ["docker"]
COMPOSE = DOCKER + ["compose", "-f", str(ROOT / "docker-compose.yml")]
MOCK = COMPOSE + ["-f", str(HERE / "compose.mock.yml")]
GRAFANA = os.getenv("GRAFANA_URL", "http://127.0.0.1:3000").rstrip("/")
KINDS = (
    "history",
    "pagination",
    "ai",
    "context",
    "mixed",
    "mock",
    "faults",
    "task",
    "recovery",
    "soak",
)


def command(args, **kwargs):
    return subprocess.run(args, cwd=ROOT, text=True, check=True, **kwargs)


def sql(db, statement):
    return command(
        COMPOSE
        + [
            "exec",
            "-T",
            "db",
            "psql",
            "-XAt",
            "-v",
            "ON_ERROR_STOP=1",
            "-U",
            "postgres",
            "-d",
            db,
        ],
        input=statement,
        capture_output=True,
    ).stdout.strip()


def api(url, payload=None, statuses=(200,)):
    data = None if payload is None else json.dumps(payload).encode()
    try:
        with urlopen(
            Request(url, data, {"Content-Type": "application/json"}), timeout=15
        ) as response:
            if response.status not in statuses:
                raise RuntimeError(f"{url}: unexpected HTTP {response.status}")
            return json.load(response)
    except HTTPError as error:
        raise RuntimeError(f"{url}: HTTP {error.code}") from error


def fixtures(prefix, group, count, start, team):
    result = []
    for index in range(start, start + count):
        email = f"load-{prefix}-{index}@example.test"
        password = secrets.token_urlsafe(24)
        account = api(
            "http://127.0.0.1:8081/v1/auth/register",
            {"email": email, "password": password},
            (201,),
        )
        user_id = account.get("user_id")
        if type(user_id) is not int or user_id <= 0:
            raise RuntimeError("Auth returned an invalid user_id")
        if group != "history":
            today = date.today()
            api(
                "http://127.0.0.1:8084/v1/user",
                {
                    "userId": str(user_id),
                    "firstName": "Load",
                    "lastName": f"Test-{index}",
                    "teamId": team,
                    "track": "backend",
                    "startDate": today.isoformat(),
                    "plannedEndDate": (today + timedelta(days=90)).isoformat(),
                },
                (200, 201),
            )
        result.append(
            {
                "index": index,
                "email": email,
                "password": password,
                "userId": str(user_id),
                "marker": f"load-{prefix}-{index}",
            }
        )
    return result


def seed(users, messages, task=False):
    values = ", ".join(f"({user['userId']}, '{user['marker']}')" for user in users)
    proposal_sql = (
        """
    INSERT INTO chat.action (message_id, resource_id, resource_label, access_level)
    SELECT m.id, f.marker, 'Synthetic resource', 'read'
    FROM load_fixture f JOIN chat.conversation c USING (user_id)
    JOIN chat.message m ON m.conversation_id = c.id AND m.body = f.marker || '-1';
    SELECT f.user_id, a.id FROM load_fixture f JOIN chat.conversation c USING (user_id)
    JOIN chat.message m ON m.conversation_id = c.id AND m.body = f.marker || '-1'
    JOIN chat.action a ON a.message_id = m.id;
    """
        if task
        else ""
    )
    output = sql(
        "chat",
        f"""
    BEGIN;
    CREATE TEMP TABLE load_fixture(user_id bigint, marker text) ON COMMIT DROP;
    INSERT INTO load_fixture VALUES {values};
    INSERT INTO chat.conversation (user_id) SELECT user_id FROM load_fixture;
    INSERT INTO chat.message (conversation_id, role, body, sources, created_at)
    SELECT c.id, 'assistant', f.marker || '-' || n, '[]'::jsonb,
           now() + n * interval '1 millisecond'
    FROM load_fixture f JOIN chat.conversation c USING (user_id)
    CROSS JOIN generate_series(1, {messages}) AS series(n);
    {proposal_sql}
    COMMIT;
    """,
    )
    if task:
        proposals = dict(line.split("|") for line in output.splitlines() if "|" in line)
        for user in users:
            user["proposal"] = proposals[user["userId"]]


def seed_context(users):
    values = ", ".join(f"({user['userId']})" for user in users)
    sql(
        "chat",
        f"""
    BEGIN;
    CREATE TEMP TABLE load_context(user_id bigint) ON COMMIT DROP;
    INSERT INTO load_context VALUES {values};
    INSERT INTO chat.conversation (user_id) SELECT user_id FROM load_context;
    INSERT INTO chat.message (conversation_id, role, body, sources, idempotency_key, created_at)
    SELECT c.id, 'user', 'Что должно быть в первом pull request стажёра?', '[]'::jsonb, 1,
           now() - interval '2 minutes'
    FROM chat.conversation c JOIN load_context f USING (user_id);
    INSERT INTO chat.message (conversation_id, role, body, sources, created_at)
    SELECT c.id, 'assistant', 'В первом pull request есть четыре блока.', '[]'::jsonb,
           now() - interval '1 minute'
    FROM chat.conversation c JOIN load_context f USING (user_id);
    COMMIT;
    """,
    )


def dashboard_link(prefix, start, end="now"):
    query = urlencode(
        {
            "var-run_prefix": prefix,
            "from": start,
            "to": end,
            "refresh": "5s" if end == "now" else "",
        }
    )
    return f"{GRAFANA}/d/chat-load-suite/chat-load-suite?{query}"


def report_index(directory, rows, local_links=()):
    links = "".join(
        f'<tr><td>{html.escape(label)}</td><td><a href="{html.escape(url, quote=True)}">Открыть в Grafana</a></td></tr>'
        for label, url in rows
    )
    links += "".join(
        f'<tr><td>{html.escape(label)}</td><td><a href="{html.escape(target, quote=True)}">Открыть отчёт</a></td></tr>'
        for label, target in local_links
    )
    (directory / "index.html").write_text(
        '<!doctype html><html lang="ru"><meta charset="utf-8"><title>Нагрузочный прогон</title>'
        "<style>body{font:16px system-ui;margin:2rem}td{padding:.4rem 1rem;border-bottom:1px solid #ddd}</style>"
        f"<h1>Нагрузочный прогон</h1><table>{links}</table></html>",
        encoding="utf-8",
    )


def compact_metrics(path, seconds):
    trends, completed, failures, outcomes, dropped = {}, {}, {}, {}, 0
    if not path.is_file():
        return
    with path.open() as stream:
        for line in stream:
            try:
                item = json.loads(line)
            except ValueError:
                continue
            if item.get("type") != "Point":
                continue
            data = item["data"]
            tags, value = data.get("tags") or {}, data["value"]
            scenario = tags.get("scenario", "-")
            metric = item["metric"]
            if metric == "step_duration_ms":
                key = (scenario, tags.get("step", "-"), tags.get("kind", "-"))
                trends.setdefault(key, []).append(value)
            elif metric == "workflows_completed":
                completed[scenario] = completed.get(scenario, 0) + value
            elif metric == "workflow_failed":
                failures[scenario] = failures.get(scenario, 0) + value
            elif metric == "workflow_outcomes":
                key = (scenario, tags.get("kind", "-"), tags.get("reason", "-"))
                outcomes[key] = outcomes.get(key, 0) + value
            elif metric == "dropped_iterations":
                dropped += value
    lines = ["Сценарий | Завершено | Ошибки проверок | Завершено / плановую секунду"]
    for name, count in sorted(completed.items()):
        lines.append(
            f"{name} | {count:.0f} | {failures.get(name, 0):.0f} | {count / seconds:.2f}"
        )
    lines.extend(
        [
            f"Пропущено итераций: {dropped:.0f}",
            "",
            "Сценарий | Шаг | Класс | Замеров | p50 мс | p95 мс | p99 мс",
        ]
    )
    for key, values in sorted(trends.items()):
        values.sort()
        percentiles = [
            values[math.ceil(len(values) * p) - 1] for p in (0.5, 0.95, 0.99)
        ]
        lines.append(
            " | ".join((*key, str(len(values)), *(f"{p:.1f}" for p in percentiles)))
        )
    lines.extend(["", "Сценарий | Класс | Результат | Количество"])
    for key, count in sorted(outcomes.items()):
        lines.append(" | ".join((*key, str(int(count)))))
    report = "\n".join(lines) + "\n"
    (path.parent / "summary.txt").write_text(report, encoding="utf-8")
    print(report, flush=True)


def run_k6(mode, prefix, stage, config):
    stage.mkdir()
    config_path = stage / "fixtures.json"
    start = int(time.time() * 1000)
    print(f"\n{mode} — Grafana: {dashboard_link(prefix, start)}", flush=True)
    status = 1
    stats = None
    try:
        with os.fdopen(
            os.open(config_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w"
        ) as stream:
            json.dump(config | {"prefix": prefix, "mode": mode}, stream)
        if mode == "soak":
            from threading import Event, Thread

            stop_stats = Event()

            def record_stats():
                with (stage / "container-stats.log").open("w") as log:
                    while not stop_stats.is_set():
                        try:
                            sample = command(
                                COMPOSE + ["stats", "--no-stream", "--format", "json"],
                                capture_output=True,
                                timeout=10,
                            ).stdout
                        except (OSError, subprocess.SubprocessError) as error:
                            sample = f"stats error: {error}\n"
                        log.write(f"\n{int(time.time() * 1000)}\n{sample}")
                        log.flush()
                        stop_stats.wait(30)

            stats = Thread(target=record_stats, daemon=True)
            stats.start()
        with (stage / "k6.log").open("w") as stream:
            status = subprocess.run(
                [
                    "k6",
                    "run",
                    "--quiet",
                    "--out",
                    f"json={stage / 'metrics.json'}",
                    "--console-output",
                    str(stage / "events.log"),
                    str(HERE / "suite.js"),
                ],
                cwd=ROOT,
                env=os.environ | {"LOAD_CONFIG": str(config_path)},
                stdout=stream,
                stderr=subprocess.STDOUT,
                text=True,
            ).returncode
    finally:
        config_path.unlink(missing_ok=True)
        if stats is not None:
            stop_stats.set()
            stats.join(timeout=11)
    end = int(time.time() * 1000)
    link = dashboard_link(prefix, start, end)
    report_index(
        stage,
        [("Все запросы", link)]
        + [
            (
                f"{group} · {user['index']} · Auth user {user['userId']}",
                dashboard_link(prefix + f"{user['index']:04x}", start, end),
            )
            for group, members in config["users"].items()
            for user in members
        ],
        [
            ("Сводка", "summary.txt"),
            ("События k6", "events.log"),
            ("Журнал k6", "k6.log"),
        ],
    )
    compact_metrics(stage / "metrics.json", config["seconds"])
    print(
        f"Дашборд: {link}\nОтчёт: {stage / 'index.html'}\nk6: код {status}, журнал {stage / 'k6.log'}",
        flush=True,
    )
    if status and (stage / "k6.log").exists():
        print(
            "".join((stage / "k6.log").read_text().splitlines(keepends=True)[-8:]),
            flush=True,
        )
    return status, link


def dlq_offsets():
    totals = {}
    for topic in ("task.access_requests.create-dlq", "task.status.changed-dlq"):
        output = command(
            COMPOSE
            + [
                "exec",
                "-T",
                "kafka",
                "/opt/kafka/bin/kafka-get-offsets.sh",
                "--bootstrap-server",
                "kafka:9092",
                "--topic",
                topic,
            ],
            capture_output=True,
        ).stdout
        offsets = [
            int(line.rsplit(":", 1)[1])
            for line in output.splitlines()
            if line.startswith(topic + ":")
        ]
        if not offsets:
            raise RuntimeError(f"Cannot read Kafka offsets for {topic}")
        totals[topic] = sum(offsets)
    return totals


def check_task_rows(users, expected):
    ids = ",".join(user["proposal"] for user in users)
    output = sql(
        "task_db",
        f"""
        SELECT proposal_id, user_id, status, resource_id, access_level
        FROM task.access_request WHERE proposal_id IN ({ids});
    """,
    )
    rows = [line.split("|") for line in output.splitlines()]
    if not expected:
        if rows:
            raise RuntimeError("Task already has rows for the queued fixtures")
        return
    actual = sorted(tuple(row) for row in rows)
    wanted = sorted(
        (
            user["proposal"],
            user["userId"],
            "approved" if i % 2 == 0 else "rejected",
            user["marker"],
            "read",
        )
        for i, user in enumerate(users)
    )
    if actual != wanted:
        raise RuntimeError(
            f"Task audit failed: {len(actual)} rows for {len(wanted)} fixtures or inconsistent fields"
        )


def ready():
    deadline = time.monotonic() + 60
    while True:
        try:
            if api("http://127.0.0.1:8000/readyz").get("status") == "ready":
                return
        except (OSError, RuntimeError):
            pass
        if time.monotonic() >= deadline:
            raise RuntimeError("AI /readyz did not become ready within 60 seconds")
        time.sleep(2)


def mock_start():
    command(MOCK + ["up", "-d", "--wait", "load-db", "load-provider"])
    command(MOCK + ["--profile", "tools", "run", "--rm", "--no-deps", "ai-migrate"])
    command(
        MOCK
        + [
            "run",
            "--rm",
            "--no-deps",
            "ai",
            "/app/.venv/bin/python",
            "-m",
            "ai_service.rag.import_documents",
            "/load-documents.json",
        ]
    )
    command(MOCK + ["up", "-d", "--no-deps", "ai"])
    ready()
    actual = command(
        MOCK + ["exec", "-T", "ai", "printenv", "LLM_BASE_URL", "EMBEDDING_BASE_URL"],
        capture_output=True,
    ).stdout.splitlines()
    if actual != ["http://load-provider:8080/v1"] * 2:
        raise RuntimeError("AI is not configured to use the local provider")
    print("AI uses load-provider for LLM and embeddings.", flush=True)


def mock_stop():
    try:
        command(COMPOSE + ["up", "-d", "--no-deps", "ai"])
    finally:
        command(MOCK + ["stop", "load-provider", "load-db"])


def settings(mode, rate, seconds, team, prefix, history_rps):
    groups = {}
    decider = ""
    size, pages = (100, 5) if mode == "pagination" else (5, 1)

    if mode in ("history", "pagination", "mixed", "mock", "soak"):
        groups["history"] = fixtures(prefix, "history", 100, 1, team)
        seed(groups["history"], size)

    amount = math.ceil(rate * seconds / 60)

    if mode in ("ai", "context", "mixed", "mock", "faults", "soak"):
        if mode == "soak":
            # Reserve a slot for every local VU ID, including the history pool.
            amount = 60 + max(12, math.ceil(rate * 90 / 60) + 4)

        groups["question"] = fixtures(prefix, "question", amount, 101, team)
        probes = 3 if mode in ("ai", "mixed") else 1
        groups["probe"] = fixtures(prefix, "probe", probes, 101 + amount, team)

        if mode == "context":
            seed_context(groups["question"] + groups["probe"])

    if mode in ("task", "recovery"):
        groups["task"] = fixtures(prefix, "task", amount, 1, team)
        seed(groups["task"], 1, task=True)
        decider = fixtures(prefix, "decider", 1, amount + 1, team)[0]["userId"]

    return {
        "rate": rate,
        "seconds": seconds,
        "team": team,
        "users": groups,
        "messages": size,
        "pages": pages,
        "historyRps": history_rps,
        "taskToken": os.getenv("TASK_TRACKER_TOKEN", ""),
        "decider": decider,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("test", choices=(*KINDS, "all"))
    parser.add_argument(
        "--offline",
        action="store_true",
        help="Use the local provider (also the default)",
    )
    parser.add_argument("--rates", help="Comma-separated target rates for one test")
    parser.add_argument("--seconds", type=int, help="Seconds per stage for one test")
    parser.add_argument(
        "--history-rps",
        type=int,
        default=100,
        help="Read rate during mixed scenarios",
    )
    args = parser.parse_args()

    if not 1 <= args.history_rps <= 200:
        parser.error("--history-rps must be between 1 and 200")
    if shutil.which("k6") is None:
        parser.error("k6 was not found in PATH")

    tests = list(KINDS) if args.test == "all" else [args.test]

    team = os.getenv("LOAD_TEAM_ID", "")
    if any(test not in ("history", "pagination") for test in tests):
        if not (team.isascii() and team.isdecimal() and 0 < int(team) < 2**63):
            parser.error("LOAD_TEAM_ID must identify an existing test team")
        team = str(int(team))
        if (
            sql("users", f"SELECT team_id FROM users.teams WHERE team_id={team};")
            != team
        ):
            parser.error("LOAD_TEAM_ID does not exist in User")

    if any(test in ("task", "recovery") for test in tests):
        if not os.getenv("TASK_TRACKER_TOKEN"):
            parser.error("Set TASK_TRACKER_TOKEN for the Task scenarios")

    profiles = {
        "history": [(25, 120), (50, 120), (100, 120), (200, 120)],
        "pagination": [(25, 120), (50, 120), (100, 120)],
        "ai": [(15, 180), (30, 180), (60, 180)],
        "context": [(30, 180)],
        "mixed": [(30, 300)],
        "mock": [(60, 300)],
        "faults": [(60, 180)],
        "task": [(30, 120), (60, 120), (120, 120)],
        "recovery": [(60, 60)],
        "soak": [(60, 1800)],
    }

    if args.rates or args.seconds is not None:
        if args.test in ("all", "recovery"):
            parser.error("Rate overrides apply to individual tests except recovery")

        rates = (
            [int(value) for value in args.rates.split(",")]
            if args.rates
            else [rate for rate, _ in profiles[args.test]]
        )
        if any(rate < 1 or rate > 200 for rate in rates) or (
            args.seconds is not None and not 1 <= args.seconds <= 3600
        ):
            parser.error("Use rates from 1 to 200 and durations from 1 to 3600 seconds")

        seconds = args.seconds or profiles[args.test][0][1]
        if args.test in ("ai", "context", "mixed", "mock", "faults", "task") and any(
            math.ceil(rate * seconds / 60) > 500 for rate in rates
        ):
            parser.error(
                "This profile needs more than 500 single-use test accounts per stage"
            )

        profiles[args.test] = [(rate, seconds) for rate in rates]

    suite = secrets.token_hex(6)
    directory = Path.home() / "k6-reports" / f"chat-suite-{suite}"
    directory.mkdir(parents=True, mode=0o700)
    begin = int(time.time() * 1000)
    print(
        f"Дашборд набора: {dashboard_link(suite, begin)}\nОтчёты: {directory}",
        flush=True,
    )

    rows, failed = [], []
    stage_number = 0

    for test in tests:
        with_mock = test in ("ai", "context", "mixed", "mock", "faults", "soak")
        try:
            if with_mock:
                mock_start()

            for rate, seconds in profiles[test]:
                stage_number += 1
                prefix = suite + f"{stage_number:04x}"
                stage = directory / f"{stage_number:02}-{test}-{rate}"
                print(
                    f"Подготовка {test}, нагрузка {rate}, длительность {seconds} с",
                    flush=True,
                )
                config = settings(test, rate, seconds, team, prefix, args.history_rps)
                before = dlq_offsets() if test in ("task", "recovery") else {}

                if before:
                    after_id = sql(
                        "task_db",
                        "SELECT coalesce(max(id), 0) FROM task.access_request "
                        f"WHERE team_id={team};",
                    )
                    if not after_id.isdecimal():
                        raise RuntimeError("Task returned an invalid starting cursor")
                    config["taskAfter"] = after_id

                if test == "recovery":
                    command(COMPOSE + ["stop", "task"])
                    try:
                        status, link = run_k6("queue", prefix, stage, config)
                        rows.append(("recovery / queue", link))
                        check_task_rows(config["users"]["task"], False)
                    finally:
                        command(COMPOSE + ["up", "-d", "--no-deps", "--wait", "task"])

                    stage_number += 1
                    drain_prefix = suite + f"{stage_number:04x}"
                    drain_status, link = run_k6(
                        "drain",
                        drain_prefix,
                        stage.with_name(stage.name + "-drain"),
                        config,
                    )
                    status = status or drain_status
                else:
                    status, link = run_k6(
                        "history" if test == "pagination" else test,
                        prefix,
                        stage,
                        config,
                    )

                rows.append((f"{test} / {rate} — k6 {status}", link))
                if status:
                    raise RuntimeError(f"k6 returned {status}")

                if test in ("task", "recovery"):
                    check_task_rows(config["users"]["task"], True)
                    if dlq_offsets() != before:
                        raise RuntimeError("Kafka DLQ offsets grew during the test")

        except (
            OSError,
            ValueError,
            KeyError,
            RuntimeError,
            subprocess.CalledProcessError,
        ) as error:
            failed.append(test)
            print(f"{test}: {error}", file=sys.stderr)
        finally:
            if with_mock:
                try:
                    mock_stop()
                except (OSError, subprocess.CalledProcessError) as error:
                    failed.append(f"{test}: restore AI")
                    print(f"Restore AI failed: {error}", file=sys.stderr)

    end = int(time.time() * 1000)
    suite_link = dashboard_link(suite, begin, end)
    report_index(
        directory,
        [("Весь набор", suite_link), *rows],
        [
            (item.name, f"{item.name}/index.html")
            for item in sorted(directory.iterdir())
            if (item / "index.html").exists()
        ],
    )
    print(f"Дашборд набора: {suite_link}\nОглавление: {directory / 'index.html'}")
    print(
        "Не прошли: " + ", ".join(failed)
        if failed
        else "Все выбранные сценарии прошли."
    )
    return bool(failed)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"Load suite: {error}", file=sys.stderr)
        sys.exit(1)

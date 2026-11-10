"""Measure the exact SQL on a disposable schema in AI_TEST_POSTGRES_DSN."""

import asyncio
import json
import os
from statistics import median, quantiles
from time import perf_counter
from uuid import uuid4

from psycopg import AsyncConnection, sql
from psycopg.conninfo import make_conninfo

from ai_service.repositories.knowledge import SEARCH_SQL, KnowledgeRepository
from ai_service.schemas.contracts import Actor

from test_rag import PROFILE, VECTOR, doc, migration_sql, pool_for


async def benchmark() -> None:
    dsn = os.environ["AI_TEST_POSTGRES_DSN"]
    schema = f"ai_bench_{uuid4().hex}"
    identifier = sql.Identifier(schema)
    scoped = make_conninfo(dsn, options=f"-csearch_path={schema},public")
    async with await AsyncConnection.connect(dsn, autocommit=True) as admin:
        await admin.execute(sql.SQL("CREATE SCHEMA {}").format(identifier))
        try:
            async with await AsyncConnection.connect(scoped) as db:
                for version in (2, 3):
                    await db.execute(migration_sql(version))
            async with pool_for(scoped) as pool:
                repository = KnowledgeRepository(scoped, pool)
                for count in (100, 1000):
                    teams = ["51"] * (count * 9 // 10) + ["52"] * (count // 100)
                    teams += ["53"] * (count - len(teams))
                    await repository.replace(
                        [doc(f"doc-{index:04}", team=team) for index, team in enumerate(teams)],
                        [[1.0, index / count, *VECTOR[2:]] for index in range(count)],
                        "benchmark-model", PROFILE,
                    )
                    async with pool.connection() as db:
                        await db.execute("ANALYZE documents, document_chunks, document_access")

                    for team in ("51", "52", "54"):
                        actor = Actor(user_id="42", role="intern", team=team)
                        params = {
                            "profile": PROFILE, "role": actor.role, "team": team, "vector": json.dumps(VECTOR),
                            "limit": 3, "max_distance": 0.7, "snippet": 1200,
                        }
                        async with pool.connection() as db:
                            cursor = await db.execute("EXPLAIN (ANALYZE, BUFFERS) " + SEARCH_SQL, params)
                            plan = "\n".join(row["QUERY PLAN"] for row in await cursor.fetchall())
                        samples = []
                        for index in range(105):
                            start = perf_counter()
                            result = await repository.search(VECTOR, PROFILE, actor, 0.3)
                            if index >= 5:
                                samples.append((perf_counter() - start) * 1000)
                            assert len(result.hits) == min(3, teams.count(team))
                        print(
                            f"{count} fragments, {teams.count(team) / count:.0%} visible: "
                            f"p50={median(samples):.2f} ms p95={quantiles(samples, n=20)[18]:.2f} ms"
                        )
                        print(plan)
        finally:
            await admin.execute(sql.SQL("DROP SCHEMA {} CASCADE").format(identifier))


if __name__ == "__main__":
    asyncio.run(benchmark())

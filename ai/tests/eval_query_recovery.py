"""Opt-in live LLM eval; synthetic retrieval, no database or embedding API calls.

Run from ai: uv run --env-file ../.env python tests/eval_query_recovery.py
This is deliberately not collected by pytest: it spends the configured LLM API budget.
"""

import argparse
import asyncio
import json
import re
from dataclasses import dataclass
from importlib.resources import files
from pathlib import Path
from time import monotonic

import httpx

from ai_service.agents.agent import Agent, NO_KNOWLEDGE
from ai_service.clients.llm import ResponsesModel
from ai_service.core.config import Settings
from ai_service.schemas.clarifications import CLARIFICATION_QUESTIONS
from ai_service.schemas.contracts import AccessPolicy, Actor, SearchHit, SearchResult


REPOSITORY = "https://git.example.test/team/repo"
REPOSITORY_POLICY = AccessPolicy(repository_host="git.example.test", resource_label="Git", access_levels=["Developer"])

GIT = SearchHit(
    id="synthetic-git", title="Доступ к Git (синтетический тест)",
    text="Для доступа к Git отправьте заявку наставнику через учебный портал. Срок — два рабочих дня.",
    access_policies=[REPOSITORY_POLICY],
)
WIFI = SearchHit(id="synthetic-wifi", title="Wi-Fi", text="Офисная сеть Wi-Fi называется TestGuest.")
INTERNAL = SearchHit(
    id="synthetic-internal", title="synthetic-service-17",
    text="Доступ к сервису synthetic-service-17 выдаёт наставник через учебный портал.",
    access_policies=[AccessPolicy(resource_id="synthetic-service-17", resource_label="synthetic-service-17", access_levels=["read"])],
)
NO_VPN = SearchHit(
    id="synthetic-no-vpn", title="Git без VPN",
    text="Для доступа к Git без VPN отправьте заявку наставнику через учебный портал.",
    access_policies=[REPOSITORY_POLICY],
)


@dataclass(frozen=True)
class Case:
    name: str
    question: str
    hits: tuple[SearchHit, ...] = (GIT,)
    refined_hits: tuple[SearchHit, ...] | None = None
    outcome: str = "grounded_answer"
    query_terms: tuple[str, ...] = ("git",)
    answer_terms: tuple[str, ...] = ()
    reformulate: bool = True


CASES = (
    Case("correct", f"Как получить доступ к Git? {REPOSITORY}", reformulate=False),
    Case("typo", f"Как палучить достпу к Git? {REPOSITORY}"),
    Case("english", f"How do I get Git access? {REPOSITORY}", reformulate=False),
    Case("translit", f"kak poluchit dostup k Git {REPOSITORY}"),
    Case("mixed", f"как poluchit достпу к Git {REPOSITORY}"),
    Case("layout", f"rfr gjkexbnm ljcneg r Git {REPOSITORY}"),
    Case("irrelevant-first-result", f"kak poluchit dostup k Git {REPOSITORY}", hits=(WIFI,), refined_hits=(GIT,)),
    Case("preserve-name", "kak poluchit dostup k synthetic-service-17", hits=(INTERNAL,),
         query_terms=("synthetic-service-17",)),
    Case("preserve-negation", f"kak poluchit dostup k Git bez VPN {REPOSITORY}", hits=(NO_VPN,),
         query_terms=("git", "vpn", "без")),
    Case("unknown-repository", "Как получить доступ к репозиторию?", hits=(), outcome="clarification", query_terms=(), answer_terms=()),
    Case("ambiguous", "нжн дстп", hits=(), outcome="clarification", query_terms=(), answer_terms=()),
    Case("no-knowledge", f"kak poluchit dostup k Git {REPOSITORY}", hits=(),
         outcome="no_knowledge", answer_terms=()),
)


class ControlledRetriever:
    def __init__(self, case):
        self.case = case
        self.queries = []

    async def search(self, query, actor):
        assert actor.role == "intern" and actor.team == "synthetic"
        self.queries.append(query)
        hits = self.case.hits
        if len(self.queries) > 1 and self.case.refined_hits is not None:
            hits = self.case.refined_hits
        return SearchResult(hits=list(hits))


class CountedModel:
    def __init__(self, model):
        self.model = model
        self.calls = 0

    async def respond(self, **kwargs):
        self.calls += 1
        return await self.model.respond(**kwargs)


async def evaluate(case, settings, http, prompt):
    retriever = ControlledRetriever(case)
    model = CountedModel(ResponsesModel(http, settings.llm_base_url, settings.llm_api_key, settings.llm_model))
    agent = Agent(model, retriever, prompt)
    started = monotonic()
    result = {"case": case.name, "passed": False}
    try:
        deadline = asyncio.get_running_loop().time() + settings.chat_timeout_seconds
        async with asyncio.timeout_at(deadline):
            response = await agent.answer(
                case.question, Actor(user_id="synthetic", role="intern", team="synthetic"), deadline=deadline,
            )
        problems = []
        expected_searches = 0 if case.outcome == "clarification" else (2 if case.refined_hits is not None else 1)
        allowed_searches = range(1, 4) if case.outcome == "no_knowledge" else (expected_searches,)
        if len(retriever.queries) not in allowed_searches:
            problems.append("unexpected_search_count")
        if model.calls != len(retriever.queries) + 1:
            problems.append("unexpected_model_count")
        if case.outcome == "grounded_answer":
            expected = case.refined_hits if case.refined_hits is not None else case.hits
            if {source.id for source in response.sources} != {hit.id for hit in expected}:
                problems.append("unexpected_sources")
            if response.proposal is None:
                problems.append("missing_proposal")
            for term in case.answer_terms:
                if term.casefold() not in response.answer.casefold():
                    problems.append("missing_answer_term:" + term)
        elif case.outcome == "clarification":
            if response.answer not in CLARIFICATION_QUESTIONS or response.sources:
                problems.append("expected_clarification")
        elif response.answer != NO_KNOWLEDGE or response.sources:
            problems.append("expected_no_knowledge")
        if expected_searches:
            query = retriever.queries[0].casefold() if retriever.queries else ""
            if case.reformulate and (query == case.question.casefold() or not re.search("[а-я]", query)):
                problems.append("first_query_not_reformulated")
            for term in case.query_terms:
                if term.casefold() not in query:
                    problems.append("missing_query_term:" + term)
        result.update(passed=not problems, problems=problems, answer=response.answer,
                      proposal=response.proposal.model_dump() if response.proposal else None)
    except Exception as error:
        # Do not print provider response bodies, connection strings or credentials.
        result.update(error=type(error).__name__, reason=getattr(error, "reason", "failed"))
    result.update(queries=retriever.queries, llm_calls=model.calls, duration_ms=round((monotonic() - started) * 1000))
    return result


async def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--case", action="append", choices=[case.name for case in CASES])
    parser.add_argument("--case-delay", type=float, default=30, help="Seconds between cases to avoid provider rate limits")
    args = parser.parse_args()
    if args.case_delay < 0:
        parser.error("--case-delay must be nonnegative")
    cases = [case for case in CASES if args.case is None or case.name in args.case]
    settings = Settings.from_env()
    if not settings.llm_api_key or not settings.llm_model:
        parser.error("Configure LLM_API_KEY and LLM_MODEL in the environment.")
    prompt = files("ai_service.agents").joinpath("prompts", "system.txt").read_text(encoding="utf-8")
    results = []
    async with httpx.AsyncClient(timeout=25) as http:
        for index, case in enumerate(cases):
            if index:
                await asyncio.sleep(args.case_delay)
            result = await evaluate(case, settings, http, prompt)
            results.append(result)
            print(f"{case.name}: {'PASS' if result['passed'] else 'FAIL'} "
                  f"llm={result['llm_calls']} searches={len(result['queries'])} "
                  f"ms={result['duration_ms']}", flush=True)
            if result.get("reason") == "http_429":
                print("Stopped on provider rate limit; remaining cases were not run.", flush=True)
                break
    report = {"model": settings.llm_model, "retrieval": "controlled_synthetic", "results": results}
    if args.output:
        args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    passed = sum(result["passed"] for result in results)
    print(f"Passed {passed}/{len(cases)}; embeddings and PostgreSQL were not evaluated.")
    return 0 if passed == len(cases) else 1


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))

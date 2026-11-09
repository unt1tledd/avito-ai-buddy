import re
from pathlib import Path

from pydantic import Field, TypeAdapter

from .contracts import Actor, Identifier, SearchHit, SearchResult


class Document(SearchHit):
    roles: set[Identifier] = Field(min_length=1)
    teams: set[Identifier] = Field(default_factory=set)
    keywords: list[str] = Field(default_factory=list)


def _terms(text: str) -> set[str]:
    return set(re.findall(r"[^\W_]{3,}", text.casefold().replace("ё", "е")))


class Knowledge:
    def __init__(self, documents: list[Document]):
        self.documents = documents

    @classmethod
    def load(cls, path: Path) -> "Knowledge":
        return cls(TypeAdapter(list[Document]).validate_json(path.read_bytes()))

    def search(self, query: str, actor: Actor) -> SearchResult:
        terms = _terms(query)
        matches = []
        for document in self.documents:
            if actor.role not in document.roles or (document.teams and actor.team not in document.teams):
                continue
            title_terms = _terms(" ".join([document.title, *document.keywords]))
            score = 3 * len(terms & title_terms) + len(terms & _terms(document.text))
            if score:
                matches.append((score, document))
        matches.sort(key=lambda match: (-match[0], match[1].id))
        return SearchResult(hits=[
            SearchHit(id=document.id, title=document.title, url=document.url, text=document.text)
            for _, document in matches[:3]
        ])

from typing import Annotated

from mcp.server import MCPServer
from mcp.server.mcpserver.exceptions import ToolError
from pydantic import StringConstraints

from .contracts import ActionResult, Actor, Identifier, SearchResult
from .onboarding_client import Onboarding, OnboardingError
from .rag import Knowledge, KnowledgeError


def build_server(
    knowledge: Knowledge, onboarding: Onboarding, actor: Actor, confirmed_step_id: str | None
) -> MCPServer:
    server = MCPServer("AI-buddy")

    @server.tool()
    async def find_instruction(
        query: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=400)]
    ) -> SearchResult:
        """Ищет доступные стажёру инструкции. Передавай ключевые слова, например «окружение»."""
        try:
            return await knowledge.search(query, actor)
        except KnowledgeError as error:
            raise ToolError(str(error)) from error

    if confirmed_step_id is not None:
        @server.tool()
        async def complete_onboarding_step(step_id: Identifier) -> ActionResult:
            """Отмечает подтверждённый шаг адаптации выполненным."""
            if step_id != confirmed_step_id:
                raise ToolError("Этот шаг не был подтверждён пользователем.")
            try:
                return await onboarding.complete_step(actor, step_id)
            except OnboardingError as error:
                raise ToolError(str(error)) from error

    return server

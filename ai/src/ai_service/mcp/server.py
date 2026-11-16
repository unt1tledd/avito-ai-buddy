from mcp.server import MCPServer
from mcp.server.mcpserver.exceptions import ToolError

from ..rag.retriever import Retriever
from ..rag.tools import (
    FIND_INSTRUCTION_DESCRIPTION,
    FIND_INSTRUCTION_NAME,
    InstructionQuery,
)
from ..repositories.knowledge import KnowledgeError
from ..schemas.contracts import Actor, SearchResult


def build_server(knowledge: Retriever, actor: Actor) -> MCPServer:
    server = MCPServer("AI-buddy")

    @server.tool(name=FIND_INSTRUCTION_NAME, description=FIND_INSTRUCTION_DESCRIPTION)
    async def find_instruction(query: InstructionQuery) -> SearchResult:
        try:
            return await knowledge.search(query, actor)
        except KnowledgeError as error:
            raise ToolError(str(error)) from error

    return server

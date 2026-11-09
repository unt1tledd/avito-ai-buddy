import asyncio

import psycopg
from pydantic import ValidationError

from .contracts import Actor
from .user_client import HttpUser, UserError


class ChatProfiles:
    def __init__(self, dsn: str, user: HttpUser):
        self.dsn = dsn
        self.user = user

    async def get_actor(self, conversation_id: str, user_id: str) -> Actor:
        try:
            async with asyncio.timeout(15), await psycopg.AsyncConnection.connect(self.dsn) as db:
                await db.set_isolation_level(psycopg.IsolationLevel.READ_COMMITTED)
                actor = await self._find(db, conversation_id, user_id)
                if actor is not None:
                    return actor

                # First messages for the same chat may reach different workers.
                await db.execute(
                    "SELECT pg_advisory_xact_lock(hashtextextended(%s, 0))",
                    (f"ai-chat-profile:{conversation_id}:{user_id}",),
                )
                actor = await self._find(db, conversation_id, user_id)
                if actor is not None:
                    return actor

                actor = await self.user.get_actor(user_id)
                await db.execute(
                    """
                    INSERT INTO ai_chat_profiles (conversation_id, user_id, team, role)
                    VALUES (%s, %s, %s, %s)
                    """,
                    (conversation_id, user_id, actor.team, actor.role),
                )
                return actor
        except (psycopg.Error, TimeoutError, ValidationError) as error:
            raise UserError("Не удалось загрузить профиль для диалога.") from error

    @staticmethod
    async def _find(db: psycopg.AsyncConnection, conversation_id: str, user_id: str) -> Actor | None:
        cursor = await db.execute(
            "SELECT team, role FROM ai_chat_profiles WHERE conversation_id = %s AND user_id = %s",
            (conversation_id, user_id),
        )
        row = await cursor.fetchone()
        if row is None:
            return None
        team, role = row
        return Actor(user_id=user_id, team=team, role=role)

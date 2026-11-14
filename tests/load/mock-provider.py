import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Provider(BaseHTTPRequestHandler):
    def do_POST(self):
        try:
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if self.path == "/v1/embeddings":
                vector = [1.0] + [0.0] * (body.get("dimensions", 1536) - 1)
                self.reply(
                    200,
                    {
                        "data": [
                            {"index": i, "embedding": vector}
                            for i, _ in enumerate(body["input"])
                        ]
                    },
                )
                return
            if self.path != "/v1/responses":
                self.reply(404, {"error": "unknown route"})
                return
            messages = body["input"]
            last_user = next(
                (item for item in reversed(messages) if item.get("role") == "user"), {}
            )
            encoded = json.dumps(last_user)
            follow_up = (
                last_user.get("content") == "А какие четыре блока в нём должны быть?"
            )
            has_history = any(
                isinstance(item.get("content"), str)
                and item["content"].startswith("Предыдущие сообщения")
                and "Что должно быть в первом pull request стажёра?" in item["content"]
                for item in messages
            )
            if follow_up and not has_history:
                self.reply(400, {"error": "missing seeded context"})
                return
            result = next(
                (
                    item
                    for item in reversed(messages)
                    if item.get("type") == "function_call_output"
                ),
                None,
            )
            if "[load:429]" in encoded:
                self.reply(429, {"error": "injected rate limit"})
                return
            if "[load:500]" in encoded:
                self.reply(500, {"error": "injected provider error"})
                return
            if "[load:timeout]" in encoded:
                time.sleep(65)
            elif "[load:slow]" in encoded:
                time.sleep(6 if result is None else 9)
            else:
                time.sleep(0.05)
            if result is None:
                output = {
                    "type": "function_call",
                    "call_id": "load-search",
                    "name": "find_instruction",
                    "arguments": json.dumps({"query": "тестовая инструкция"}),
                }
            else:
                hits = json.loads(result["output"]).get("hits", [])
                answer = (
                    {
                        "kind": "grounded_answer",
                        "answer": f"Тестовая инструкция. [source:{hits[0]['id']}]",
                    }
                    if hits
                    else {"kind": "no_knowledge", "answer": "Инструкция отсутствует."}
                )
                output = {
                    "type": "message",
                    "role": "assistant",
                    "content": [
                        {
                            "type": "output_text",
                            "text": json.dumps(answer, ensure_ascii=False),
                        }
                    ],
                }
            self.reply(200, {"status": "completed", "output": [output]})
        except (ValueError, KeyError, TypeError) as error:
            self.reply(400, {"error": type(error).__name__})

    def reply(self, status, payload):
        data = json.dumps(payload).encode()
        try:
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError):
            pass


if __name__ == "__main__":
    ThreadingHTTPServer(
        ("0.0.0.0", int(os.environ.get("PORT", "8080"))), Provider
    ).serve_forever()

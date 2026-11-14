"""Deterministic external AI boundary; all business services remain real."""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/api/v1/answer":
            self.send_error(404)
            return
        if self.headers.get("Authorization") != "Bearer integration-ai-token":
            self.send_error(401)
            return
        request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if not request.get("actor", {}).get("user_id") or not request["actor"].get("team"):
            self.send_error(400, "missing user context")
            return
        answer = {"answer": "answer:" + request["message"], "sources": []}
        if request["message"] == "access":
            answer["proposal"] = {"resource_id": "integration-wiki", "resource_label": "Wiki", "access_level": "read"}
        body = json.dumps(answer).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()

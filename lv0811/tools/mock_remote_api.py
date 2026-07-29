"""Тестовый API для LinkVideo Monitor 0.8.11.

Запуск:
    python mock_remote_api.py

Endpoint:
    http://127.0.0.1:18098/api/monitor/sync
"""
from http.server import BaseHTTPRequestHandler, HTTPServer
import json

REVISION = 1
SETTINGS = {
    "protocol": "rtsp",
    "fps": 15,
    "bitrate_kbps": 1024,
    "codec": "h264",
    "encoder": "libx264",
    "audio_enabled": False,
    "microphone_enabled": False,
    "microphone_mode": "always",
    "microphone_voice_db": -42,
    "microphone_ptt_hotkey": "Ctrl+Alt+Space",
    "microphone_toggle_hotkey": "Ctrl+Alt+M",
    "overlay_enabled": True,
}
COMMAND = None  # Например: {"id": 2, "action": "restart_stream"}


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/api/monitor/sync":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        request = json.loads(self.rfile.read(length) or b"{}")
        print("\nAPI version:", request.get("api_version"))
        print("Monitor:", request.get("client", {}).get("computer_name"))
        print("Streaming:", request.get("state", {}).get("streaming"))
        print("Current settings:", request.get("settings"))
        print("Available encoders:", request.get("capabilities", {}).get("encoders"))
        response = {
            "success": True,
            "revision": REVISION,
            "settings": SETTINGS,
            "command": COMMAND,
        }
        data = json.dumps(response, ensure_ascii=False).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    print("Mock API: http://127.0.0.1:18098/api/monitor/sync")
    HTTPServer(("127.0.0.1", 18098), Handler).serve_forever()

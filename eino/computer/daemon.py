"""
Eino Computer Use Daemon
========================
在本地 localhost:9876 上暴露桌面控制 HTTP 接口，供 Go 后端调用。

启动方式：
  pip install -r requirements.txt
  python daemon.py

API：
  POST /screenshot      截图，返回 {"ok":true, "base64":"...", "width":1920, "height":1080}
  POST /click           点击 {"x":100, "y":200, "button":"left"}
  POST /move            移动鼠标 {"x":100, "y":200}
  POST /drag            拖拽 {"x1":100, "y1":200, "x2":300, "y2":400, "button":"left"}
  POST /type_text       打字 {"text":"hello world", "interval":0.05}
  POST /press_key       按键 {"key":"enter"}
  POST /scroll          滚动 {"amount":-3}
  POST /double_click    双击 {"x":100, "y":200}
  GET  /screen_size     返回 {"ok":true, "width":1920, "height":1080}
  GET  /health          健康检查
"""

import json
import io
import base64
import traceback
from http.server import HTTPServer, BaseHTTPRequestHandler

HOST = "127.0.0.1"
PORT = 9876

# ---- 懒加载，避免导入时就需要 pip install ----
_pyautogui = None
_mss = None
_sct = None

def _ensure_pyautogui():
    global _pyautogui
    if _pyautogui is None:
        import pyautogui as pg
        pg.FAILSAFE = True
        _pyautogui = pg
    return _pyautogui

def _ensure_mss():
    global _mss, _sct
    if _mss is None:
        import mss
        _mss = mss
        _sct = mss.mss()
    return _mss, _sct


def run_action(name, data):
    """分发到对应处理函数，统一返回 (status, body_dict)"""
    if name == "screenshot":
        return handle_screenshot(data)
    if name == "click":
        return handle_click(data)
    if name == "move":
        return handle_move(data)
    if name == "drag":
        return handle_drag(data)
    if name == "type_text":
        return handle_type_text(data)
    if name == "press_key":
        return handle_press_key(data)
    if name == "scroll":
        return handle_scroll(data)
    if name == "double_click":
        return handle_double_click(data)
    if name == "screen_size":
        return handle_screen_size()
    return 400, {"error": f"unsupported desktop action: {name}"}


def handle_screenshot(_data):
    """截取主显示器全屏，返回 base64 PNG。"""
    try:
        import tempfile, os as _os
        mss_mod, sct = _ensure_mss()
        monitor = sct.monitors[1]  # 主显示器
        img = sct.grab(monitor)
        # mss 10.x 的 to_png 需要文件路径；写到临时文件再读回
        fd, tmp_path = tempfile.mkstemp(suffix=".png", prefix="eino_shot_")
        _os.close(fd)
        mss_mod.tools.to_png(img.rgb, img.size, output=tmp_path)
        with open(tmp_path, "rb") as f:
            b64 = base64.b64encode(f.read()).decode("ascii")
        _os.unlink(tmp_path)
        return 200, {
            "ok": True,
            "base64": b64,
            "width": img.width,
            "height": img.height,
        }
    except Exception as e:
        return 500, {"error": f"screenshot failed: {e}"}


def handle_click(data):
    try:
        pg = _ensure_pyautogui()
        x, y = int(data["x"]), int(data["y"])
        btn = data.get("button", "left")
        pg.click(x, y, button=btn)
        return 200, {"ok": True, "action": "click", "x": x, "y": y, "button": btn}
    except Exception as e:
        return 500, {"error": f"click failed: {e}"}


def handle_double_click(data):
    try:
        pg = _ensure_pyautogui()
        x, y = int(data["x"]), int(data["y"])
        pg.doubleClick(x, y)
        return 200, {"ok": True, "action": "double_click", "x": x, "y": y}
    except Exception as e:
        return 500, {"error": f"double_click failed: {e}"}


def handle_move(data):
    try:
        pg = _ensure_pyautogui()
        x, y = int(data["x"]), int(data["y"])
        pg.moveTo(x, y)
        return 200, {"ok": True, "action": "move", "x": x, "y": y}
    except Exception as e:
        return 500, {"error": f"move failed: {e}"}


def handle_drag(data):
    try:
        pg = _ensure_pyautogui()
        x1, y1 = int(data["x1"]), int(data["y1"])
        x2, y2 = int(data["x2"]), int(data["y2"])
        btn = data.get("button", "left")
        pg.moveTo(x1, y1)
        pg.drag(x2 - x1, y2 - y1, duration=0.5, button=btn)
        return 200, {"ok": True, "action": "drag", "from": [x1, y1], "to": [x2, y2]}
    except Exception as e:
        return 500, {"error": f"drag failed: {e}"}


def handle_type_text(data):
    try:
        pg = _ensure_pyautogui()
        text = data["text"]
        interval = float(data.get("interval", 0.05))
        pg.typewrite(text, interval=interval)
        return 200, {"ok": True, "action": "type_text", "length": len(text)}
    except Exception as e:
        return 500, {"error": f"type_text failed: {e}"}


def handle_press_key(data):
    try:
        pg = _ensure_pyautogui()
        key = data["key"]
        pg.press(key)
        return 200, {"ok": True, "action": "press_key", "key": key}
    except Exception as e:
        return 500, {"error": f"press_key failed: {e}"}


def handle_scroll(data):
    try:
        pg = _ensure_pyautogui()
        amount = int(data.get("amount", -3))
        pg.scroll(amount)
        return 200, {"ok": True, "action": "scroll", "amount": amount}
    except Exception as e:
        return 500, {"error": f"scroll failed: {e}"}


def handle_screen_size():
    try:
        pg = _ensure_pyautogui()
        w, h = pg.size()
        return 200, {"ok": True, "width": w, "height": h}
    except Exception as e:
        return 500, {"error": f"screen_size failed: {e}"}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        # 静默，不打印到控制台（调试时可注释下一行）
        pass

    def _send_json(self, code, body):
        payload = json.dumps(body, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        if self.path == "/health":
            self._send_json(200, {"ok": True, "service": "eino-computer-daemon"})
        elif self.path == "/screen_size":
            code, body = handle_screen_size()
            self._send_json(code, body)
        elif self.path == "/screenshot":
            code, body = handle_screenshot({})
            self._send_json(code, body)
        else:
            self._send_json(404, {"error": "not found"})

    def do_POST(self):
        action = self.path.lstrip("/")
        try:
            length = int(self.headers.get("Content-Length", 0))
            raw = self.rfile.read(length) if length > 0 else b"{}"
            data = json.loads(raw.decode("utf-8")) if raw else {}
        except json.JSONDecodeError:
            self._send_json(400, {"error": "invalid json"})
            return

        try:
            code, body = run_action(action, data)
        except Exception:
            body = {"error": traceback.format_exc()}
            code = 500

        self._send_json(code, body)


def main():
    print(f"Eino Computer Use Daemon starting on {HOST}:{PORT}...")
    server = HTTPServer((HOST, PORT), Handler)
    print(f"  Screenshot & desktop control ready at http://{HOST}:{PORT}")
    print(f"  Press Ctrl+C to stop.")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nDaemon stopped.")
        server.server_close()


if __name__ == "__main__":
    main()

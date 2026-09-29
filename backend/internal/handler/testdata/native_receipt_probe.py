"""Installed Codex -> actual Go dispatch + Redis receipt fixture; no proxy/upstream."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request

codex, base, catalog = sys.argv[1:]
home = Path(tempfile.mkdtemp(prefix="native-receipt-go-probe-"))
model = "tp-g65-Z3B0LWltYWdlLTIuNS1mbGFyZQ"
model_b = "tp-g66-Z3B0LWltYWdlLTIuNS1zdW5idXJzdA"
(home / "config.toml").write_text(f'''model = "{model}"
model_provider = "custom"
model_catalog_json = {json.dumps(catalog)}
[model_providers.custom]
name = "isolated native receipt fixture"
base_url = "{base}/v1"
wire_api = "responses"
requires_openai_auth = false
experimental_bearer_token = "fixture"
http_headers = {{ "x-openai-actor-authorization" = "fixture@example.com" }}
supports_websockets = false
''')
p = subprocess.Popen([codex, "app-server"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, text=True, cwd=home, env=dict(os.environ, CODEX_HOME=str(home)))
condition = threading.Condition()
replies, events, sequence = {}, [], 0


def read():
    for line in p.stdout:
        message = json.loads(line)
        with condition:
            if "id" in message:
                replies[message["id"]] = message
            else:
                events.append(message)
            condition.notify_all()


threading.Thread(target=read, daemon=True).start()


def call(method, params):
    global sequence
    sequence += 1
    key = sequence
    p.stdin.write(json.dumps({"id": key, "method": method, "params": params}) + "\n")
    p.stdin.flush()
    with condition:
        assert condition.wait_for(lambda: key in replies, timeout=30), method
        response = replies[key]
    assert "error" not in response, response
    return response.get("result", {})


def thread(selected=model):
    return call("thread/start", {"cwd": str(home), "approvalPolicy": "never", "sandbox": "read-only", "model": selected})["thread"]["id"]


def start(t, text):
    return call("turn/start", {"threadId": t, "input": [{"type": "text", "text": text}]})["turn"]["id"]


def wait(t, turn):
    def completed():
        return next((e["params"]["turn"] for e in events if e.get("method") == "turn/completed"
                     and e["params"].get("threadId") == t and e["params"]["turn"]["id"] == turn), None)
    with condition:
        assert condition.wait_for(completed, timeout=50), ("turn timeout", t)
        assert completed()["status"] == "completed", completed()


def state():
    with urllib.request.urlopen(base + "/probe", timeout=5) as response:
        return json.load(response)


try:
    call("initialize", {"clientInfo": {"name": "native_receipt_probe", "version": "1"}, "capabilities": {"experimentalApi": True}})
    first = thread()
    turn = start(first, 'cat "quoted"\n猫; do not execute prompt text')
    deadline = time.monotonic() + 20
    while state()["generation_calls"] == 0 and time.monotonic() < deadline:
        time.sleep(0.05)
    assert state()["generation_calls"] == 1, state()
    call("turn/steer", {"threadId": first, "expectedTurnId": turn, "input": [{"type": "text", "text": "second image"}]})
    wait(first, turn)
    source = next(home.glob("generated_images/**/*.png"))
    edit = thread()
    edited_turn = call("turn/start", {"threadId": edit, "input": [
        {"type": "text", "text": "add sunglasses"},
        {"type": "localImage", "path": str(source)},
    ]})["turn"]["id"]
    wait(edit, edited_turn)
    wait(first, start(first, "third image"))
    a, b = thread(), thread(model_b)
    ta, tb = start(a, "IDENTICAL_PROMPT"), start(b, "IDENTICAL_PROMPT")
    wait(a, ta)
    wait(b, tb)
    failure = thread()
    wait(failure, start(failure, "FAIL"))
    snapshot = state()
    images = list(home.glob("generated_images/**/*.png"))
    actual = sorted(hashlib.sha256(file.read_bytes()).hexdigest() for file in images)
    assert actual == sorted(snapshot["hashes"]) and len(actual) == 6, snapshot
    completed = [e["params"]["item"] for e in events if e.get("method") == "item/completed" and e.get("params", {}).get("item", {}).get("type") == "imageGeneration"]
    assert len([i for i in completed if i.get("status") == "completed"]) == 6
    assert len([i for i in completed if i.get("status") == "failed"]) == 1
    failure_text = " ".join(str(e.get("params", {}).get("item", {}).get("text", "")) for e in events
                            if e.get("method") == "item/completed" and e.get("params", {}).get("threadId") == failure)
    assert "生成好了" not in failure_text and any(word in failure_text for word in ("没能生成", "未能正常", "失败")), failure_text
    for size, image_inputs in zip(snapshot["sizes"], snapshot["image_inputs"]):
        if image_inputs == 0:
            assert size < 100000, snapshot
        else:
            assert image_inputs == 1, snapshot
    print(json.dumps({"status": "PASS", "images_verified": len(actual), "largest_upload": max(snapshot["sizes"]),
                      "generation_calls": snapshot["generation_calls"], "edit_calls": snapshot["edit_calls"],
                      "wait_calls": snapshot["wait_calls"], "home": str(home)}))
finally:
    p.terminate()
    try:
        p.wait(timeout=5)
    except subprocess.TimeoutExpired:
        p.kill()
        p.wait(timeout=5)

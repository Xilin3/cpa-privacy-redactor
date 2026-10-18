"""Exercise the shared library through the exact language-neutral C ABI."""
import base64
import concurrent.futures
import ctypes
import json
import sys


PLUGIN_PATH = sys.argv[1] if len(sys.argv) > 1 else "./privacy-redactor.so"


class Buffer(ctypes.Structure):
    _fields_ = [("ptr", ctypes.c_void_p), ("len", ctypes.c_size_t)]


class Host(ctypes.Structure):
    _fields_ = [("abi_version", ctypes.c_uint32), ("ctx", ctypes.c_void_p), ("call", ctypes.c_void_p), ("free", ctypes.c_void_p)]


class Plugin(ctypes.Structure):
    _fields_ = [("abi_version", ctypes.c_uint32), ("call", ctypes.c_void_p), ("free", ctypes.c_void_p), ("shutdown", ctypes.c_void_p)]


library = ctypes.CDLL(PLUGIN_PATH)
library.cliproxy_plugin_init.argtypes = [ctypes.POINTER(Host), ctypes.POINTER(Plugin)]
library.cliproxy_plugin_init.restype = ctypes.c_int
host, plugin = Host(1, None, None, None), Plugin()
assert library.cliproxy_plugin_init(ctypes.byref(host), ctypes.byref(plugin)) == 0
assert plugin.abi_version == 1 and plugin.call and plugin.free
call = ctypes.CFUNCTYPE(ctypes.c_int, ctypes.c_char_p, ctypes.c_void_p, ctypes.c_size_t, ctypes.POINTER(Buffer))(plugin.call)
free = ctypes.CFUNCTYPE(None, ctypes.c_void_p, ctypes.c_size_t)(plugin.free)


def rpc(method, request):
    raw = json.dumps(request).encode()
    source = ctypes.create_string_buffer(raw)
    response = Buffer()
    assert call(method.encode(), source, len(raw), ctypes.byref(response)) == 0
    try:
        result = json.loads(ctypes.string_at(response.ptr, response.len))
        assert result["ok"], result.get("error", {}).get("code")
        return result["result"]
    finally:
        free(response.ptr, response.len)


registration = rpc("plugin.register", {})
assert registration["schema_version"] == 6 and registration["capabilities"]["request_normalizer"]
assert all(registration["metadata"].get(k) for k in ("Name", "Version", "Author", "GitHubRepository"))
body = json.dumps({"messages": [{"role": "system", "content": "# Environment\nOS Version: Windows 11 Home China 10.0.26200\nPrimary working directory: C:/Users/synthetic/项目\nPlatform: win32\nShell: bash"}]}).encode()


def exercise(_):
    result = rpc("request.normalize", {"FromFormat": "claude", "ToFormat": "claude", "Model": "claude-sonnet-5-5", "Body": base64.b64encode(body).decode()})
    decoded = base64.b64decode(result["Body"])
    assert b"Home China" not in decoded and b"synthetic" not in decoded
    assert b"Windows 11" in decoded and b"win32" in decoded and b"bash" in decoded
    return True


with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
    assert all(pool.map(exercise, range(40)))
status = rpc("management.handle", {"Method": "GET", "Path": "/v0/management/privacy-redactor/status"})
counters = json.loads(base64.b64decode(status["Body"]))
assert counters["changed_requests"] >= 40
print(json.dumps({"abi": "passed", "parallel_calls": 40, "schema": registration["schema_version"], "counters": counters}), flush=True)

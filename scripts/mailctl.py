#!/usr/bin/env python3
"""PMail独立进程管理。仅管理本目录记录的PID，不清理其他端口或进程。"""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent
BINARY = ROOT / "bin/pmail"
PIDFILE = ROOT / "runtime/pmail.pid.json"
os.umask(0o077)


def process_record(pid):
    result = subprocess.run(["/bin/ps", "-ww", "-p", str(pid), "-o", "lstart=", "-o", "command="], capture_output=True, text=True)
    return result.stdout.strip() if result.returncode == 0 else ""


def owned_process():
    if not PIDFILE.exists():
        return None
    record = json.loads(PIDFILE.read_text())
    actual = process_record(record["pid"])
    if not actual:
        return None
    if actual != record["identity"] or not actual.endswith(" " + str(BINARY) + " -p " + str(record["http_port"])):
        raise RuntimeError("PID身份不符，拒绝操作；请人工检查")
    return record["pid"]


def main():
    action = sys.argv[1] if len(sys.argv) > 1 else "status"
    if action not in ("start", "stop", "status"):
        raise RuntimeError("用法: python3 mailctl.py start|stop|status")
    pid = owned_process()
    if action == "status":
        print("running pid=" + str(pid) if pid else "stopped")
        return
    if action == "stop":
        if not pid:
            print("already stopped")
            return
        os.kill(pid, signal.SIGTERM)
        for _ in range(100):
            if not process_record(pid):
                PIDFILE.unlink(missing_ok=True)
                print("stopped")
                return
            time.sleep(0.1)
        raise RuntimeError("退出超时，未强制杀进程；请检查日志")
    if pid:
        print("already running pid=" + str(pid))
        return
    cfg = json.loads((ROOT / "config/config.json").read_text())
    endpoints = [(cfg["httpBind"], cfg["httpPort"])]
    if not cfg.get("disableSMTP", False):
        endpoints.append((cfg["smtpBind"], cfg["smtpPort"]))
    for address, port in endpoints:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
            # 与Go监听器一致，允许复用TIME_WAIT；仍拒绝已有监听者。
            probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            probe.bind((address, port))
    for folder in ("runtime", "logs"):
        (ROOT / folder).mkdir(mode=0o700, exist_ok=True)
    # 默认资源目标可由调用者环境覆盖；GOMEMLIMIT不是RSS硬上限。
    env = dict(os.environ, PMail_ROOT=str(ROOT))
    env.setdefault("GOMAXPROCS", "2")
    env.setdefault("GOMEMLIMIT", "128MiB")
    with (ROOT / "logs/pmail.log").open("ab") as log:
        process = subprocess.Popen([str(BINARY), "-p", str(cfg["httpPort"])], cwd=ROOT, env=env,
                                   stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
    time.sleep(0.3)
    if process.poll() is not None:
        raise RuntimeError("启动失败，查看logs/pmail.log")
    PIDFILE.write_text(json.dumps({"pid": process.pid, "identity": process_record(process.pid), "http_port": cfg["httpPort"]}))
    print("started pid=" + str(process.pid))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)

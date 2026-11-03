#!/usr/bin/env python3
"""Real native-client IPC against our unprivileged daemon; never activates networking."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

root = Path(__file__).resolve().parent.parent
runtime = root / "build" / f"ipc-{os.getpid()}"
runtime.mkdir(mode=0o700, parents=True)
socket = runtime / "control.sock"
log = runtime / "daemon.log"
try:
    with log.open("wb") as output:
        # This child remains attached to this bounded supervisor and is always reaped.
        process = subprocess.Popen([str(root / "build/tailmultid"), "--state-dir", str(runtime / "state"), "--socket", str(socket)], stdout=output, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 10
            while not socket.exists():
                if process.poll() is not None or time.monotonic() > deadline:
                    raise RuntimeError("Preview daemon did not start: " + log.read_text())
                time.sleep(0.05)
            env = dict(os.environ, TAILMULTI_TEST_SOCKET=str(socket))
            result = subprocess.run([str(root / "scripts/swift.sh"), "test", "--filter", "LivePreviewProtocolTests"], cwd=root, env=env, timeout=120)
            if result.returncode:
                raise RuntimeError("Native IPC integration failed")
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    print("PASS: native Swift client -> owner-authenticated Unix socket -> real preview daemon. No enrollment or network activation.")
finally:
    shutil.rmtree(runtime)

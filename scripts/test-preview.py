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
            def cli(*args, code=0):
                done = subprocess.run([str(root / "build/tailmulti"), "--socket", str(socket), *args], capture_output=True, text=True, timeout=30)
                if done.returncode != code:
                    raise RuntimeError(f"tailmulti {args[0]} exited {done.returncode}: {done.stderr}")
                return done.stdout + done.stderr
            if "Preview service" not in cli("status"):
                raise RuntimeError("Command-line status did not report the preview service")
            cli("account", "add", "Command Line")
            if "Command Line" not in cli("account", "list"):
                raise RuntimeError("Command-line account was not saved")
            # Preview has no network capability, so nothing may connect, sign in or start.
            cli("connect", "command line", code=1)
            cli("login", "command line", code=1)
            cli("up", code=1)
            cli("account", "remove", "command line")
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    print("PASS: native Swift client and tailmulti command -> owner-authenticated Unix socket -> real preview daemon. No enrollment or network activation.")
finally:
    shutil.rmtree(runtime)

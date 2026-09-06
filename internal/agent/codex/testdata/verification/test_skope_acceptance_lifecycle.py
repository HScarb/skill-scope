"""Exercise only run() with controlled Python children; no Codex or namespaces."""
import ast
import ctypes
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest

if sys.platform != "linux":
    raise SystemExit("Linux required for process-group and /proc assertions")
# Adopt killed grandchildren so the test itself can verify and reap their exit.
if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
    raise OSError(ctypes.get_errno(), "PR_SET_CHILD_SUBREAPER")

SOURCE = Path(__file__).with_name("skope_acceptance.py")
TREE = ast.parse(SOURCE.read_text())
RUN = next(node for node in TREE.body if isinstance(node, ast.FunctionDef) and node.name == "run")
CHILD = """import os, subprocess, sys, time
from pathlib import Path
child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'])
Path(os.environ['CHILD_PID_FILE']).write_text(str(child.pid))
time.sleep(60)
"""


class LifecycleTests(unittest.TestCase):
    def exercise(self, mode):
        with tempfile.TemporaryDirectory(prefix="skope-lifecycle-", dir="/var/tmp") as directory:
            root = Path(directory)
            pid_file = root / "child.pid"
            processes = []
            descendants = []
            original = PermissionError("controlled observation failure")
            timeouts = []

            def popen(*args, **kwargs):
                proc = subprocess.Popen(*args, **kwargs)
                processes.append(proc)
                if mode != "normal":
                    deadline = time.monotonic() + 5
                    while not pid_file.exists() and time.monotonic() < deadline:
                        time.sleep(0.01)
                    self.assertTrue(pid_file.exists(), "controlled child did not start")
                    descendants.append(int(pid_file.read_text()))
                communicate = proc.communicate
                if mode in ("timeout", "exited-parent"):
                    def bounded(*args, **kwargs):
                        if not timeouts:
                            kwargs["timeout"] = 0.05
                        try:
                            return communicate(*args, **kwargs)
                        except subprocess.TimeoutExpired as error:
                            timeouts.append(error)
                            raise
                    proc.communicate = bounded
                return proc

            def observed_path(path):
                if mode == "observation" and str(path).startswith("/proc/"):
                    def fail(**kwargs):
                        raise original
                    return SimpleNamespace(resolve=fail)
                return Path(path)

            namespace = dict(SKOPE=sys.executable, CODEX_EXE=Path(sys.executable).resolve(),
                             ROOT=root, REPO=root, ENV={"CHILD_PID_FILE": str(pid_file)},
                             RESULTS=[], unchanged=lambda: None, Path=observed_path,
                             subprocess=SimpleNamespace(Popen=popen, PIPE=subprocess.PIPE,
                                                        DEVNULL=subprocess.DEVNULL),
                             os=os, signal=signal, time=time, json=json)
            exec(compile(ast.Module(body=[RUN], type_ignores=[]), str(SOURCE), "exec"), namespace)
            args = ["-c", "print('normal child')" if mode == "normal" else CHILD]
            if mode == "exited-parent":
                args[1] = CHILD.rsplit("time.sleep(60)", 1)[0]
            if mode == "observation":
                args.append("features")
            try:
                if mode == "normal":
                    result = namespace["run"](mode, args)
                    self.assertEqual(result["stdout"], "normal child\n")
                    self.assertEqual(result["exit"], 0)
                else:
                    with self.assertRaises(PermissionError if mode == "observation" else subprocess.TimeoutExpired) as caught:
                        namespace["run"](mode, args)
                    self.assertIs(caught.exception, original if mode == "observation" else timeouts[0])
                proc = processes[0]
                self.assertIsNotNone(proc.poll(), "run() left its direct child alive")
                self.assertFalse(Path(f"/proc/{proc.pid}").exists(), "direct child was not reaped")
                for pid in list(descendants):
                    deadline = time.monotonic() + 3
                    reaped = 0
                    while not reaped and time.monotonic() < deadline:
                        reaped, status = os.waitpid(pid, os.WNOHANG)
                        if not reaped:
                            time.sleep(0.01)
                    self.assertEqual(reaped, pid, "run() left a descendant alive")
                    descendants.remove(pid)
                    self.assertEqual(os.waitstatus_to_exitcode(status), -signal.SIGKILL)
                    self.assertFalse(Path(f"/proc/{pid}").exists())
            finally:
                # Make the failing/red version reproducible without leaking children.
                for pid in descendants:
                    try:
                        os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                for proc in processes:
                    if proc.poll() is None:
                        proc.kill()
                    proc.communicate()
                for pid in descendants:
                    try:
                        os.waitpid(pid, 0)
                    except ChildProcessError:
                        pass

    def test_timeout_reaps_child_and_stops_descendant(self):
        self.exercise("timeout")

    def test_observation_error_reaps_child_and_stops_descendant(self):
        self.exercise("observation")

    def test_normal_child_preserves_output_and_exit(self):
        self.exercise("normal")

    def test_exited_parent_still_terminates_pipe_holding_descendant(self):
        self.exercise("exited-parent")


if __name__ == "__main__":
    unittest.main()

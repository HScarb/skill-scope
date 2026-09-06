"""Real Linux skope -> Codex prompt loading, without network or credentials.

Run inside unshare --user --map-root-user --mount --net, passing absolute
skope and Codex executables. Results remain in a unique /var/tmp directory.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

if sys.platform != "linux":
    raise SystemExit("Linux required; HOME does not isolate Windows Known Folder")
uid_map = Path("/proc/self/uid_map").read_text().split()
if uid_map[0] != "0" or uid_map[2] != "1":
    raise SystemExit("Run in a private mapped-root user/mount/network namespace")
interfaces = [line.split(":")[0].strip() for line in Path("/proc/net/dev").read_text().splitlines()[2:]]
if interfaces != ["lo"]:
    raise SystemExit("Private network namespace with only loopback required")
SKOPE, CODEX_EXE = (Path(p).resolve(strict=True) for p in sys.argv[1:3])
assert hashlib.sha256(CODEX_EXE.read_bytes()).hexdigest() == "b9315df68cb0e2827c940ffacb66f7524e820d9744060c755fbf938724ad2b76"
subprocess.run(["mount", "--make-rprivate", "/"], check=True)
subprocess.run(["mount", "-t", "tmpfs", "skope-acceptance", "/etc"], check=True)
ROOT = Path(tempfile.mkdtemp(prefix="skope-phase3-acceptance-", dir="/var/tmp"))
print(ROOT, flush=True)
HOME, CODEX, REPO, CONFIG, CLAUDE = (ROOT / n for n in ("home", "codex", "repo", "skope", "claude"))
for p in (HOME, CODEX, REPO, CONFIG, CLAUDE):
    p.mkdir()
ENV = {"PATH": "/usr/bin:/bin", "HOME": str(HOME), "USERPROFILE": str(HOME),
       "CODEX_HOME": str(CODEX), "SKOPE_HOME": str(CONFIG), "CLAUDE_CONFIG_DIR": str(CLAUDE)}
subprocess.run(["git", "init", str(REPO)], env=ENV, check=True, capture_output=True)
PROTECTED = {}
RESULTS = []
OLD_NS = 981173106000000000


def write(path, body):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding="utf-8")
    os.utime(path, ns=(OLD_NS, OLD_NS))
    PROTECTED[path] = (path.read_bytes(), path.stat().st_mtime_ns)


def skill(root, name):
    path = root / "SKILL.md"
    write(path, f"---\nname: {name}\ndescription: Isolated Phase 3 acceptance.\n---\nPHASE3_{name}\n")
    return path


def unchanged():
    for path, before in PROTECTED.items():
        assert (path.read_bytes(), path.stat().st_mtime_ns) == before, str(path)


def run(label, args, expected=0):
    argv = [str(SKOPE), *args]
    proc = subprocess.Popen(argv, cwd=REPO, env=ENV, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    delegate = None
    if "prompt-input" in args or "features" in args:
        for _ in range(500):
            try:
                executable = Path(f"/proc/{proc.pid}/exe").resolve(strict=True)
                if executable == CODEX_EXE:
                    delegate = {"exe": str(executable), "argv": Path(f"/proc/{proc.pid}/cmdline").read_bytes().decode().rstrip("\0").split("\0")}
                    break
            except FileNotFoundError:
                break
            time.sleep(0.01)
    stdout, stderr = proc.communicate(timeout=45)
    record = {"case": label, "argv": argv, "pid": proc.pid, "exit": proc.returncode,
              "stdout": stdout, "stderr": stderr, "delegate": delegate}
    RESULTS.append(record)
    (ROOT / "results.json").write_text(json.dumps(RESULTS, indent=2), encoding="utf-8")
    assert proc.returncode == expected, record
    if "prompt-input" in args or "features" in args:
        assert delegate is not None, "Did not observe real Codex executable after handoff"
        if args[args.index("-s") + 1] != "none":
            assert delegate["argv"][-2:] == ["-c", "features.remote_plugin=false"], delegate
    unchanged()
    print(f"PASS {label}", flush=True)
    return record


def sessions():
    return list((CONFIG / "sessions").glob("*/owner.json"))


def prompt(label, selection):
    record = run(label, ["codex", "-s", selection, "--", "debug", "prompt-input", "fixture prompt"])
    if selection != "none":
        owners = sessions()
        assert len(owners) == 1, owners
        owner = json.loads(owners[0].read_text())
        assert owner["pid"] == record["pid"] and owner["agent"] == "codex", owner
        assert owner["skillSet"] == selection and owner["processStart"], owner
        assert sorted(p.name for p in owners[0].parent.iterdir()) == ["owner.json"]
        record["owner"] = owner
    messages = json.loads(record["stdout"][record["stdout"].index("\n[") + 1:])
    texts = "\n".join(content["text"] for message in messages for content in message.get("content", [])
                      if content.get("type") == "input_text")
    roots = dict(re.findall(r"- `(r\d+)` = `([^`]+)`", texts))
    for alias, root in roots.items():
        texts = texts.replace("(file: " + alias + "/", "(file: " + root + "/")
    return texts


def visible(output, path, enabled):
    # Check the actual prompt skill entry by its canonical file path, not names
    # alone (two fixture skills intentionally share a frontmatter name).
    entries = {Path(entry).resolve() for entry in re.findall(r"\(file: ([^)]+)\)", output)}
    assert (path.resolve() in entries) == enabled, (path, enabled)


write(CONFIG / "config.toml", f'version=1\n[agents.codex]\ncommand={json.dumps(str(CODEX_EXE))}\n')
allowed = skill(HOME / ".agents/skills/selected", "p3-same")
same = skill(REPO / ".agents/skills/selected", "p3-same")
blocked = skill(CODEX / "skills/blocked", "p3-same")
target = skill(ROOT / "target", "p3-linked")
for name in ("linked", "linked-alias"):
    (HOME / ".agents/skills" / name).symlink_to(target.parent, target_is_directory=True)
plugin_paths = {}
for plugin in ("permit", "deny"):
    root = CODEX / "plugins/cache/fixture" / plugin / "1.0.0"
    write(root / ".codex-plugin/plugin.json", json.dumps({"name": plugin, "version": "1.0.0"}))
    plugin_paths[plugin] = [skill(root / "skills" / name, f"p3-{plugin}-{name}") for name in ("one", "two")]
denied_only = skill(CODEX / "plugins/cache/fixture/deny/1.0.0/skills/denied-only", "p3-denied-only")
plugin_paths["deny"].append(denied_only)
auto_root = HOME / ".agents/skills/auto-root"
write(auto_root / ".codex-plugin/plugin.json", '{"name":"p3-auto"}')
auto = skill(auto_root, "p3-auto-root")
auto_child = skill(auto_root / "skills/child", "p3-auto-child")
foreign = skill(CLAUDE / "skills/foreign", "p3-foreign")
write(CLAUDE / "commands/command.md", "fixture command")
write(CONFIG / "skillsets.toml", '''version=1
[skillsets.phase3]
skills=["selected","linked","foreign","command","denied-only","missing"]
plugins.codex=["permit@fixture","missing@fixture"]
bundled=false
[skillsets.bundled]
skills=["selected","linked"]
plugins.codex=["permit@fixture"]
bundled=true
[skillsets.empty]
skills=[]
bundled=false
[skillsets.auto]
skills=["auto-root","child"]
bundled=false
''')
BASE = '[plugins."permit@fixture"]\nenabled=false\n[plugins."deny@fixture"]\nenabled=true\n'
RULES = {
    "path": [{"path": str(allowed), "enabled": False}],
    "name": [{"name": "p3-same", "enabled": False}],
    "combined": [{"path": str(allowed), "enabled": False}, {"name": "p3-same", "enabled": False},
                 {"name": "permit:p3-permit-one", "enabled": False}],
}


def toml_rule(rule):
    return "{" + ",".join(k + "=" + ("true" if v else "false") if isinstance(v, bool)
                            else k + "=" + json.dumps(v) for k, v in rule.items()) + "}"


for label, rules in RULES.items():
    rules = [*rules, {"name": "imagegen", "enabled": False}]
    write(CODEX / "config.toml", BASE + "\n[skills]\nconfig=[" + ",".join(map(toml_rule, rules)) + "]\n")
    none = prompt(label + "-none", "none")
    assert sessions() == []
    visible(none, allowed, False)
    visible(none, blocked, label == "path")
    for path in plugin_paths["permit"]:
        visible(none, path, False)
    for path in plugin_paths["deny"]:
        visible(none, path, True)
    active = prompt(label + "-active", "phase3")
    for path in (allowed, same, target, *plugin_paths["permit"]):
        visible(active, path, True)
    for path in (blocked, auto, auto_child, foreign, *plugin_paths["deny"]):
        visible(active, path, False)
    assert str(CODEX / "skills/.system/") not in active
    dry = run(label + "-dry", ["codex", "-s", "phase3", "--dry-run"])
    assert sessions() == []
    for fragment in ("2 native, 0 projected, 3 unavailable, 1 missing", "plugin missing@fixture is not installed",
                     "features.remote_plugin=false", "bundled: off"):
        assert fragment in dry["stdout"] + dry["stderr"], fragment

out = prompt("bundled-active", "bundled")
system = CODEX / "skills/.system"
assert system.is_dir()
visible(out, system / "imagegen/SKILL.md", False)
baseline_system = {Path(entry).resolve() for entry in re.findall(r"\(file: ([^)]+)\)", none)
                   if Path(entry).is_relative_to(system)}
active_system = {Path(entry).resolve() for entry in re.findall(r"\(file: ([^)]+)\)", out)
                 if Path(entry).is_relative_to(system)}
assert baseline_system and active_system == baseline_system, (active_system, baseline_system)
visible(out, system / "openai-docs/SKILL.md", True)
out = prompt("empty-set", "empty")
for path in (allowed, same, blocked, target, auto, auto_child, *plugin_paths["permit"], *plugin_paths["deny"]):
    visible(out, path, False)
assert str(system) not in out
out = prompt("ordinary-manifest-root", "auto")
visible(out, auto, True)
visible(out, auto_child, True)
features = run("remote-feature", ["codex", "-s", "phase3", "--", "features", "list"])
remote = [line.split() for line in features["stdout"].splitlines() if line.startswith("remote_plugin")]
assert len(remote) == 1 and remote[0][-1] == "false", remote
run("final-none-reap", ["codex", "-s", "none", "--", "--version"])
assert sessions() == []

# Empty inventory uses a separate fixture tree; the previous .system cache and
# source snapshots remain intact for review.
EMPTY = ROOT / "empty"
for key, relative in (("HOME", "home"), ("USERPROFILE", "home"), ("CODEX_HOME", "codex"),
                      ("CLAUDE_CONFIG_DIR", "claude")):
    path = EMPTY / relative
    path.mkdir(parents=True, exist_ok=True)
    ENV[key] = str(path)
REPO = EMPTY / "repo"
REPO.mkdir()
empty = run("empty-inventory-dry", ["codex", "-s", "empty", "--dry-run"])
assert "skills.config=[]" in empty["stdout"]
out = prompt("empty-inventory-active", "empty")
assert "PHASE3_" not in out and "/SKILL.md" not in out
run("empty-final-reap", ["codex", "-s", "none", "--", "--version"])
assert sessions() == []

# Preserve literal whitespace in CODEX_HOME; lexical '..' after a symlink is
# rejected because cleaning it before filesystem traversal changes the source.
space_home = EMPTY / "codex "
space_skill = skill(space_home / "skills/not-allowed", "p3-space-not-allowed")
ENV["CODEX_HOME"] = str(space_home)
visible(prompt("space-home-none", "none"), space_skill, True)
visible(prompt("space-home-active", "empty"), space_skill, False)
link_parent, actual_parent = EMPTY / "a", EMPTY / "b"
link_parent.mkdir()
(actual_parent / "child").mkdir(parents=True)
(link_parent / "link").symlink_to(actual_parent / "child", target_is_directory=True)
(link_parent / "codex").mkdir()
dotdot_skill = skill(actual_parent / "codex/skills/not-allowed", "p3-parent-not-allowed")
ENV["CODEX_HOME"] = str(link_parent / "link") + "/../codex"
visible(prompt("parent-home-none", "none"), dotdot_skill, True)
for label, args in (("parent-home-dry-rejected", ["--dry-run"]),
                    ("parent-home-launch-rejected", ["--", "--version"])):
    rejected = run(label, ["codex", "-s", "empty", *args], expected=1)
    assert "CODEX_HOME must not contain parent-directory components" in rejected["stderr"], rejected
    assert sessions() == []
ENV["CODEX_HOME"] = " \t "
rejected = run("whitespace-home-rejected", ["codex", "-s", "empty", "--dry-run"], expected=1)
assert "CODEX_HOME must be an absolute directory" in rejected["stderr"], rejected
assert sessions() == []
unchanged()
(ROOT / "results.json").write_text(json.dumps(RESULTS, indent=2), encoding="utf-8")
(ROOT / "summary.json").write_text(json.dumps({"skope": str(SKOPE), "skope_sha256": hashlib.sha256(SKOPE.read_bytes()).hexdigest(),
    "codex": str(CODEX_EXE), "cases": len(RESULTS), "protected_files": len(PROTECTED),
    "assertions": "passed", "sessions_remaining": 0}, indent=2), encoding="utf-8")
print("ALL ASSERTIONS PASSED", flush=True)

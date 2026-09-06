"""Read-only Codex protocol experiment; creates only disposable fixture data."""
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import tempfile
import threading

EXE = Path(sys.argv[1]).resolve()
if os.name == "nt":
    raise SystemExit("Use Linux: Windows Known Folder ignores fixture USERPROFILE for user skills.")
ROOT = Path(tempfile.mkdtemp(prefix="skope-p3-controls-", dir="/var/tmp")).resolve()
HOME = ROOT / "home"
CODEX = ROOT / "codex"
REPO = ROOT / "repo"
for directory in (HOME, CODEX, REPO):
    directory.mkdir()
ENV = {key: value for key, value in os.environ.items()
       if key.upper() in {"PATH", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "PATHEXT", "COMSPEC"}}
ENV.update(HOME=str(HOME), USERPROFILE=str(HOME), CODEX_HOME=str(CODEX))
subprocess.run(["git", "init", str(REPO)], env=ENV, check=True, capture_output=True)

def skill(directory, name):
    directory.mkdir(parents=True, exist_ok=True)
    file = directory / "SKILL.md"
    file.write_text(f"---\nname: {name}\ndescription: Phase 3 isolated fixture.\n---\nPHASE3_{name.upper().replace('-', '_')}_MARKER\n", encoding="utf-8")
    return file

ALLOW = skill(HOME / ".agents/skills/allow", "p3-allow")
BLOCK = skill(HOME / ".agents/skills/block", "p3-block")

def quote(value):
    # JSON basic strings for fixture paths are also valid TOML basic strings.
    return json.dumps(str(value), ensure_ascii=False)

def rules(*pairs):
    return "[" + ",".join("{path=" + quote(path) + ",enabled=" + str(enabled).lower() + "}" for path, enabled in pairs) + "]"

RESULTS = []

def run(label, overrides=(), config="", prefix=()):
    config_file = CODEX / "config.toml"
    config_file.write_text(config, encoding="utf-8")
    before = config_file.read_bytes()
    argv = [str(EXE), *prefix, "app-server", "--stdio"]
    for override in overrides:
        argv.extend(["-c", override])
    process = subprocess.Popen(argv, cwd=REPO, env=ENV, stdin=subprocess.PIPE,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               text=True, encoding="utf-8")
    messages = queue.Queue()
    errors = []
    def pump():
        for line in process.stdout:
            messages.put(json.loads(line))
        messages.put(None)
    threading.Thread(target=pump, daemon=True).start()
    threading.Thread(target=lambda: errors.extend(process.stderr.readlines()), daemon=True).start()
    def send(value):
        process.stdin.write(json.dumps(value) + "\n")
        process.stdin.flush()
    def response(identifier):
        while True:
            message = messages.get(timeout=20)
            if message is None:
                raise RuntimeError("app-server exited: " + "".join(errors))
            if message.get("id") == identifier:
                return message
    result = {"case": label, "argv": argv, "config": config}
    try:
        send({"id": 1, "method": "initialize", "params": {"clientInfo": {"name": "skope_fixture", "version": "1"}}})
        result["initialize"] = response(1)
        send({"method": "initialized"})
        send({"id": 2, "method": "skills/list", "params": {"cwds": [str(REPO)], "forceReload": True}})
        result["skills"] = response(2)
        send({"id": 3, "method": "config/read", "params": {"cwd": str(REPO), "includeLayers": True}})
        result["effective_config"] = response(3)
    except Exception as error:
        result["failure"] = str(error)
    finally:
        if os.name == "nt":
            subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"], capture_output=True)
        else:
            process.kill()
        process.wait(timeout=5)
    result["config_unchanged"] = before == config_file.read_bytes()
    RESULTS.append(result)
    (ROOT / "results.json").write_text(json.dumps(RESULTS, ensure_ascii=False, indent=2), encoding="utf-8")
    summary = [{key: s.get(key) for key in ("name", "enabled", "path", "scope")}
               for group in result.get("skills", {}).get("result", {}).get("data", []) for s in group["skills"]]
    if any(not Path(s["path"]).is_relative_to(ROOT) for s in summary):
        raise RuntimeError("Unexpected skill outside fixture; stop without continuing experiments")
    print(json.dumps({"case": label, "skills": summary, "failure": result.get("failure"), "protocol_error": result.get("skills", {}).get("error")}, ensure_ascii=False), flush=True)
    return result

print(str(ROOT), flush=True)
print(subprocess.check_output([str(EXE), "--version"], env=ENV, text=True).strip(), flush=True)
for command in (["--help"], ["exec", "--help"], ["app-server", "--help"], ["app-server", "generate-json-schema", "--help"]):
    subprocess.run([str(EXE), *command], env=ENV, check=True, stdout=subprocess.DEVNULL)
subprocess.run([str(EXE), "app-server", "generate-json-schema", "--out", str(ROOT / "schema")], env=ENV, check=True)
run("baseline")
run("block-file", ["skills.config=" + rules((BLOCK, False))])
user_path = "[[skills.config]]\npath=" + quote(ALLOW) + "\nenabled=false\n"
run("user-path", config=user_path)
run("user-path-empty-cli", ["skills.config=[]"], user_path)
run("user-path-block-cli", ["skills.config=" + rules((BLOCK, False))], user_path)
run("user-path-allow-true-cli", ["skills.config=" + rules((ALLOW, True), (BLOCK, False))], user_path)
run("empty-whitelist", ["skills.config=" + rules((ALLOW, False), (BLOCK, False))])
run("block-directory", ["skills.config=" + rules((BLOCK.parent, False))])
UNICODE = skill(HOME / ".agents/skills/中文 space", "p3-unicode")
SAME = skill(HOME / ".agents/skills/other-id", "p3-allow")
run("unicode-and-duplicate-name-baseline")
run("unicode-and-duplicate-name-deny", ["skills.config=" + rules((UNICODE, False), (ALLOW, False))])
TARGET = skill(ROOT / "target", "p3-link")
LINK1 = HOME / ".agents/skills/link-one"
LINK2 = HOME / ".agents/skills/link-two"
LINK1.symlink_to(TARGET.parent, target_is_directory=True)
LINK2.symlink_to(TARGET.parent, target_is_directory=True)
run("symlink-baseline")
run("symlink-discovery-deny", ["skills.config=" + rules((LINK1 / "SKILL.md", False))])
run("symlink-canonical-deny", ["skills.config=" + rules((TARGET, False))])
user_name = '[[skills.config]]\nname="p3-allow"\nenabled=false\n'
for label, configuration in (("name", user_name), ("path-and-name", user_path + user_name)):
    run("user-" + label, config=configuration)
    run("user-" + label + "-empty-cli", ["skills.config=[]"], configuration)
    run("user-" + label + "-block-cli", ["skills.config=" + rules((BLOCK, False))], configuration)
    run("user-" + label + "-allow-true-cli", ["skills.config=" + rules((ALLOW, True), (BLOCK, False))], configuration)
run("duplicate-false-true", ["skills.config=" + rules((ALLOW, False), (ALLOW, True))])
run("duplicate-true-false", ["skills.config=" + rules((ALLOW, True), (ALLOW, False))])
project_file = REPO / ".codex/config.toml"
project_file.parent.mkdir()
project_file.write_text(user_path, encoding="utf-8")
trusted = "[projects." + quote(REPO) + ']\ntrust_level="trusted"\n'
run("project-deny", config=trusted)
run("project-deny-empty-cli", ["skills.config=[]"], trusted)
run("project-deny-true-cli", ["skills.config=" + rules((ALLOW, True), (BLOCK, False))], trusted)
project_file.unlink()
(CODEX / "fixture.config.toml").write_text(user_path, encoding="utf-8")
run("profile-v2-cli", prefix=["-p", "fixture"])
run("profile-v2-cli-allow-true", ["skills.config=" + rules((ALLOW, True), (BLOCK, False))], prefix=["-p", "fixture"])
run("default-profile-legacy", config='profile="fixture"\n[profiles.fixture]\n' + user_path.replace('[[skills.config]]', '[[profiles.fixture.skills.config]]'))
run("bundled-existing-false", ["skills.bundled.enabled=false"])
run("bundled-existing-true", ["skills.bundled.enabled=true"])
system_root = CODEX / "skills/.system"
if system_root.exists():
    system_root.rename(CODEX / "saved-system")
run("bundled-absent-false", ["skills.bundled.enabled=false"])
RESULTS[-1]["system_created"] = system_root.exists()
run("bundled-absent-true", ["skills.bundled.enabled=true"])
RESULTS[-1]["system_created"] = system_root.exists()
(ROOT / "results.json").write_text(json.dumps(RESULTS, ensure_ascii=False, indent=2), encoding="utf-8")
parser_results = []
for label, args in (
    ("interactive-tail", ["fixture prompt", "-c", 'sandbox_mode="p3-invalid"']),
    ("exec-tail", ["exec", "fixture prompt", "-c", 'sandbox_mode="p3-invalid"']),
    ("interactive-double-dash", ["--", "fixture prompt", "-c", 'sandbox_mode="p3-invalid"']),
    ("exec-double-dash", ["exec", "--", "fixture prompt", "-c", 'sandbox_mode="p3-invalid"']),
):
    process = subprocess.run([str(EXE), *args], cwd=REPO, env=ENV, input="", capture_output=True, text=True, timeout=15)
    parser_results.append({"case": label, "argv": [str(EXE), *args], "returncode": process.returncode, "stdout": process.stdout, "stderr": process.stderr})
(ROOT / "parser-results.json").write_text(json.dumps(parser_results, indent=2), encoding="utf-8")
print(json.dumps(parser_results), flush=True)
subprocess.run([str(EXE), "debug", "prompt-input", "--help"], env=ENV, check=True, stdout=subprocess.DEVNULL)
prompt_results = []
for label, config, prefix, overrides in (
    ("prompt-baseline", "", [], []),
    ("prompt-block", "", [], ["skills.config=" + rules((BLOCK, False))]),
    ("prompt-user-path-true", user_path, [], ["skills.config=" + rules((ALLOW, True), (BLOCK, False))]),
    ("prompt-user-name-true", user_name, [], ["skills.config=" + rules((ALLOW, True), (BLOCK, False))]),
    ("prompt-profile-deny", "", ["-p", "fixture"], []),
    ("prompt-profile-true", "", ["-p", "fixture"], ["skills.config=" + rules((ALLOW, True), (BLOCK, False))]),
):
    (CODEX / "config.toml").write_text(config, encoding="utf-8")
    argv = [str(EXE), *prefix, "debug", "prompt-input", "fixture prompt"]
    for override in overrides:
        argv.extend(["-c", override])
    process = subprocess.run(argv, cwd=REPO, env=ENV, capture_output=True, text=True, timeout=30)
    prompt_results.append({"case": label, "argv": argv, "returncode": process.returncode, "stdout": process.stdout, "stderr": process.stderr})
    print(label, process.returncode, "allow=" + str("/allow/SKILL.md)" in process.stdout), "block=" + str("/block/SKILL.md)" in process.stdout), flush=True)
(ROOT / "prompt-results.json").write_text(json.dumps(prompt_results, indent=2), encoding="utf-8")

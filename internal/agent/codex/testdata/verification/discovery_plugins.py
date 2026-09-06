"""Isolated Linux Codex discovery/plugin experiment; no model or credentials."""
import json
import os
from pathlib import Path
import queue
import shutil
import subprocess
import sys
import tempfile
import threading

EXE = Path(sys.argv[1]).resolve()
if os.name != "posix":
    raise SystemExit("Linux required: Windows Known Folder cannot be isolated with HOME.")
PRIVATE_ADMIN = "--private-admin" in sys.argv
if PRIVATE_ADMIN:
    uid_map = Path("/proc/self/uid_map").read_text().split()
    if uid_map[0] != "0" or uid_map[2] != "1":
        raise RuntimeError("Refuse admin fixture outside a mapped-root user namespace")
    subprocess.run(["mount", "--make-rprivate", "/"], check=True)
    subprocess.run(["mount", "-t", "tmpfs", "skope-fixture", "/etc"], check=True)
elif Path("/etc/codex").exists():
    raise SystemExit("Use a private mount namespace to isolate existing /etc/codex")
ROOT = Path(tempfile.mkdtemp(prefix="skope-p3-catalog-", dir="/var/tmp"))
HOME, CODEX, REPO = (ROOT / x for x in ("home", "codex", "repo"))
for directory in (HOME, CODEX, REPO):
    directory.mkdir()
ENV = {"PATH": os.environ["PATH"], "HOME": str(HOME), "USERPROFILE": str(HOME), "CODEX_HOME": str(CODEX)}
subprocess.run(["git", "init", str(REPO)], env=ENV, check=True, capture_output=True)
RESULTS = []

def write(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(data, encoding="utf-8")

def skill(directory, name):
    write(directory / "SKILL.md", f"---\nname: {name}\ndescription: Isolated Phase 3 fixture.\n---\nPHASE3_{name}\n")

def run(label, cwd=REPO, config="", overrides=(), requests=()):
    write(CODEX / "config.toml", config)
    argv = [str(EXE), "app-server", "--stdio", "-c", "skills.bundled.enabled=false"]
    for value in overrides:
        argv.extend(["-c", value])
    proc = subprocess.Popen(argv, cwd=cwd, env=ENV, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, text=True)
    messages, errors = queue.Queue(), []
    def pump():
        for line in proc.stdout:
            messages.put(json.loads(line))
        messages.put(None)
    threading.Thread(target=pump, daemon=True).start()
    threading.Thread(target=lambda: errors.extend(proc.stderr.readlines()), daemon=True).start()
    def call(identifier, method, params):
        proc.stdin.write(json.dumps({"id": identifier, "method": method, "params": params}) + "\n")
        proc.stdin.flush()
        while True:
            message = messages.get(timeout=30)
            if message is None:
                raise RuntimeError("".join(errors))
            if message.get("id") == identifier:
                return message
    result = {"case": label, "cwd": str(cwd), "argv": argv, "config": config}
    try:
        result["initialize"] = call(1, "initialize", {"clientInfo": {"name": "skope_fixture", "version": "1"}})
        for identifier, (method, params) in enumerate(requests, 10):
            result[method] = call(identifier, method, params)
        result["skills/list"] = call(2, "skills/list", {"cwds": [str(cwd)], "forceReload": True})
        result["config/read"] = call(3, "config/read", {"cwd": str(cwd), "includeLayers": True})
    finally:
        proc.kill()
        proc.wait(timeout=5)
    result["stderr"] = "".join(errors)
    RESULTS.append(result)
    write(ROOT / "results.json", json.dumps(RESULTS, indent=2))
    entries = [s for group in result["skills/list"].get("result", {}).get("data", []) for s in group["skills"]]
    if any(not Path(s["path"]).is_relative_to(ROOT) and not s["path"].startswith("/etc/codex/") for s in entries):
        raise RuntimeError("Unexpected skill outside isolated fixture")
    print(json.dumps({"case": label, "skills": [{k: s.get(k) for k in ("name", "path", "enabled", "scope")} for s in entries],
                      "requests": {m: result[m] for m, _ in requests}}), flush=True)
    return result

print(ROOT, flush=True)
print(subprocess.check_output([str(EXE), "--version"], env=ENV, text=True).strip(), flush=True)
roots = {"user": HOME / ".agents/skills", "legacy": CODEX / "skills", "root": REPO / ".agents/skills",
         "old-root": REPO / ".codex/skills", "parent": REPO / "apps/.agents/skills",
         "cwd": REPO / "apps/api/.agents/skills", "old-cwd": REPO / "apps/api/.codex/skills",
         "sibling": REPO / "apps/web/.agents/skills", "descendant": REPO / "apps/api/deeper/.agents/skills"}
for name, root in roots.items():
    skill(root / name, "p3-" + name)
    skill(root / "group/nested", "p3-nested-" + name)
    skill(root / ".hidden", "p3-hidden-" + name)
    skill(root / "same", "p3-same")
    skill(root / "alias", "p3-same")
    skill(root / "container", "p3-container-" + name)
    skill(root / "container/child", "p3-child-" + name)
    target = ROOT / "targets" / name
    skill(target / "leaf", "p3-linked-" + name)
    (root / "link-skill").symlink_to(target / "leaf", target_is_directory=True)
    (root / "link-parent").symlink_to(target, target_is_directory=True)
run("discovery-root")
run("discovery-cwd", REPO / "apps/api")
(REPO / ".git").rename(REPO / "git-disabled")
run("discovery-no-git", REPO / "apps/api")
(REPO / "git-disabled").rename(REPO / ".git")

market = HOME / ".agents/plugins/marketplace.json"
entries = []
for name in ("alpha", "beta", "candidate"):
    plugin = HOME / "plugins" / name
    manifest = {"name": name, "version": "1.0.0"}
    if name == "beta":
        manifest["skills"] = "./custom"
    write(plugin / ".codex-plugin/plugin.json", json.dumps(manifest))
    skill(plugin / ("custom" if name == "beta" else "skills") / "one", "p3-plugin-" + name)
    skill(plugin / ("custom" if name == "beta" else "skills") / "two", "p3-same")
    entries.append({"name": name, "source": {"source": "local", "path": "./plugins/" + name},
                    "policy": {"installation": "AVAILABLE", "authentication": "ON_USE"}, "category": "Productivity"})
write(market, json.dumps({"name": "fixture", "plugins": entries}))
run("local-candidates", requests=[("plugin/list", {"cwds": [str(REPO)], "marketplaceKinds": ["local"], "forceRefetch": False})])
for name in ("alpha", "beta"):
    run("install-" + name, config=(CODEX / "config.toml").read_text(), requests=[("plugin/install", {"pluginName": name, "marketplacePath": str(market)})])
run("installed", config=(CODEX / "config.toml").read_text(), requests=[("plugin/installed", {"cwds": [str(REPO)]})])
BASE = (CODEX / "config.toml").read_text()
feature_results = []
for args in (["features", "--help"], ["features", "list"], ["features", "list", "-c", "features.remote_plugin=false"]):
    p = subprocess.run([str(EXE), *args], cwd=REPO, env=ENV, capture_output=True, text=True, timeout=30)
    feature_results.append({"argv": [str(EXE), *args], "exit": p.returncode, "stdout": p.stdout, "stderr": p.stderr})
write(ROOT / "features.json", json.dumps(feature_results, indent=2))
run("remote-feature-off", config=BASE, overrides=['features.remote_plugin=false'])
installed = [("plugin/installed", {"cwds": [str(REPO)]})]
run("disabled", config=BASE.replace("enabled = true", "enabled = false"), requests=installed)
run("no-config-retained-cache", requests=installed)
run("config-only", config='[plugins."missing@fixture"]\nenabled=true\n', requests=installed)
run("candidate-config", config='[plugins."candidate@fixture"]\nenabled=true\n', requests=installed)
for key in ('plugins."alpha@fixture".enabled', 'plugins.alpha@fixture.enabled'):
    run("override-" + key, config=BASE, overrides=[key + "=false"])
run("override-parent", config=BASE, overrides=['plugins={"alpha@fixture"={enabled=false},"beta@fixture"={enabled=true}}'])
run("override-enable", config=BASE.replace("enabled = true", "enabled = false"), overrides=['plugins.alpha@fixture.enabled=true'])
alpha = CODEX / "plugins/cache/fixture/alpha/1.0.0"
for rule in ('{path=' + json.dumps(str(alpha / "skills/one/SKILL.md")) + ',enabled=false}',
             '{name="alpha:p3-plugin-alpha",enabled=false}', '{name="p3-plugin-alpha",enabled=false}'):
    run("plugin-skill-rule-" + rule, config=BASE + '\n[skills]\nconfig=[' + rule + ']\n')
run("plugin-path-true", config=BASE + '\n[skills]\nconfig=[{name="alpha:p3-plugin-alpha",enabled=false}]\n',
    overrides=['skills.config=[{path=' + json.dumps(str(alpha / "skills/one/SKILL.md")) + ',enabled=true}]'])
manifest = HOME / "plugins/alpha/.codex-plugin/plugin.json"
saved_v1 = ROOT / "saved-alpha-v1"
shutil.copytree(alpha, saved_v1)
write(manifest, json.dumps({"name": "alpha", "version": "2.0.0"}))
skill(HOME / "plugins/alpha/skills/one", "p3-alpha-v2")
run("reinstall-v2", config=BASE, requests=[("plugin/install", {"pluginName": "alpha", "marketplacePath": str(market)})])
shutil.copytree(saved_v1, alpha)
run("multiple-versions", config=BASE, requests=installed)
for version in ("9.0.0", "10.0.0", "aaa"):
    extra = alpha.with_name(version)
    shutil.copytree(saved_v1, extra)
    skill(extra / "skills/one", "p3-version-" + version)
run("version-order", config=BASE)
os.utime(alpha.with_name("10.0.0"), (2000000000, 2000000000))
run("version-order-mtime", config=BASE)
alpha.with_name("aaa").rename(ROOT / "saved-aaa")
run("version-order-numeric", config=BASE)
empty = alpha.with_name("zzz")
empty.mkdir()
run("version-empty-highest", config=BASE)
shutil.copytree(saved_v1, alpha.with_name("local"))
run("version-local-priority", config=BASE)
alpha.with_name("local").rename(ROOT / "saved-local")
empty.rename(ROOT / "saved-zzz")
(ROOT / "saved-aaa").rename(alpha.with_name("aaa"))
for version in ("9.0.0", "10.0.0", "aaa"):
    alpha.with_name(version).rename(ROOT / ("saved-" + version))
write(manifest, json.dumps({"name": "alpha", "version": "1.0.0"}))
run("source-version-reverted", config=BASE, requests=installed)
manifest.rename(manifest.with_suffix(".disabled"))
run("source-manifest-missing", config=BASE, requests=installed)
manifest.with_suffix(".disabled").rename(manifest)
active_v2 = alpha.with_name("2.0.0")
active_v2.rename(ROOT / "saved-alpha-v2")
run("active-root-missing", config=BASE, requests=installed)
(ROOT / "saved-alpha-v2").rename(active_v2)
market.rename(market.with_suffix(".disabled"))
run("marketplace-missing", config=BASE, requests=installed)
market.with_suffix(".disabled").rename(market)
dev = REPO / ".agents/skills/dev"
write(dev / ".codex-plugin/plugin.json", '{"name":"dev","version":"1.0.0"}')
skill(dev / "skills/one", "p3-dev")
run("in-place-skill-dir", config=BASE, requests=installed)
run("in-place-off", config=BASE, overrides=['plugins.dev@skills-dir.enabled=false'])
run("in-place-config", config=BASE + '\n[plugins."dev@fixture"]\nenabled=true\npath=' + json.dumps(str(dev)) + '\n', requests=installed)
for name, root in roots.items():
    auto = root / ("auto-" + name)
    write(auto / ".codex-plugin/plugin.json", json.dumps({"name": "manifest-" + name, "version": "1.0.0"}))
    skill(auto / "skills/one", "p3-auto-" + name)
    skill(auto, "p3-ordinary-with-manifest-" + name)
run("automatic-root", config=BASE, requests=installed)
run("automatic-cwd", cwd=REPO / "apps/api", config=BASE)
auto_paths = [roots["root"] / "auto-root" / suffix for suffix in ("SKILL.md", "skills/one/SKILL.md")]
run("automatic-path-off", config=BASE, overrides=['skills.config=[' + ','.join('{path=' + json.dumps(str(p)) + ',enabled=false}' for p in auto_paths) + ']'])
(REPO / ".git").rename(REPO / "git-disabled")
run("automatic-no-git", cwd=REPO / "apps/api", config=BASE)
(REPO / "git-disabled").rename(REPO / ".git")
for name, content in {"no-name": "---\ndescription: Fixture\n---\nbody\n", "no-description": "---\nname: no-description\n---\nbody\n", "no-frontmatter": "body\n"}.items():
    write(REPO / ".agents/skills" / name / "SKILL.md", content)
(REPO / ".agents/skills/cycle").symlink_to(REPO / ".agents/skills", target_is_directory=True)
run("malformed-and-cycle", config=BASE)
project = REPO / ".codex/config.toml"
write(project, '[plugins."alpha@fixture"]\nenabled=false\n[plugins."project-only@fixture"]\nenabled=true\n')
run("untrusted-project", config=BASE)
trust = '\n[projects.' + json.dumps(str(REPO)) + ']\ntrust_level="trusted"\n'
run("trusted-project", config=BASE + trust)
run("trusted-cli-override", config=BASE + trust, overrides=['plugins.alpha@fixture.enabled=true'])
project.unlink()
profile = '[plugins."alpha@fixture"]\nenabled=false\n[plugins."profile-only@fixture"]\nenabled=true\n'
write(CODEX / "fixture.config.toml", profile)
prompts = []
for label, prefix, overrides in [("baseline", [], []), ("profile", ["-p", "fixture"], []),
                                 ("profile-cli", ["-p", "fixture"], ["plugins.alpha@fixture.enabled=true"])]:
    write(CODEX / "config.toml", BASE)
    argv = [str(EXE), *prefix, "debug", "prompt-input", "fixture", "-c", "skills.bundled.enabled=false"]
    for value in overrides:
        argv.extend(["-c", value])
    p = subprocess.run(argv, cwd=REPO, env=ENV, capture_output=True, text=True, timeout=30)
    prompts.append({"case": label, "argv": argv, "exit": p.returncode, "stdout": p.stdout, "stderr": p.stderr})
write(ROOT / "prompts.json", json.dumps(prompts, indent=2))
run("bundled-on", config=BASE, overrides=['skills.bundled.enabled=true'])
run("bundled-user-name-deny", config=BASE + '\n[skills]\nconfig=[{name="imagegen",enabled=false}]\n', overrides=['skills.bundled.enabled=true'])
system_path = CODEX / "skills/.system/imagegen/SKILL.md"
run("bundled-user-path-deny", config=BASE + '\n[skills]\nconfig=[{path=' + json.dumps(str(system_path)) + ',enabled=false}]\n', overrides=['skills.bundled.enabled=true'])
for version in ("1.0.0", "2.0.0"):
    alpha.with_name(version).rename(ROOT / ("semver-saved-" + version))
for version in ("3.0.0-alpha.2", "3.0.0-alpha.10"):
    shutil.copytree(saved_v1, alpha.with_name(version))
run("semver-prerelease", config=BASE)
shutil.copytree(saved_v1, alpha.with_name("3.0.0"))
run("semver-release", config=BASE)
for version in ("3.0.0+2", "3.0.0+10"):
    shutil.copytree(saved_v1, alpha.with_name(version))
run("semver-build", config=BASE)
for p in list(alpha.parent.iterdir()):
    p.rename(ROOT / ("semver-result-" + p.name))
for version in ("1.0.0", "2.0.0"):
    (ROOT / ("semver-saved-" + version)).rename(alpha.with_name(version))
run("uninstall-alpha", config=BASE, requests=[("plugin/uninstall", {"pluginId": "alpha@fixture"})])
if not alpha.exists():
    shutil.copytree(saved_v1, alpha)
run("uninstalled-cache-restored", config=(CODEX / "config.toml").read_text(), requests=installed)
if PRIVATE_ADMIN:
    skill(Path("/etc/codex/skills/admin"), "p3-admin")
    write(Path("/etc/codex/config.toml"), '[plugins."alpha@fixture"]\nenabled=false\n[plugins."admin-only@fixture"]\nenabled=true\n')
    run("admin-layer", config='[plugins."beta@fixture"]\nenabled=true\n')
    run("admin-user-override", config=BASE)
    run("admin-cli-override", config='[plugins."beta@fixture"]\nenabled=true\n', overrides=['plugins.alpha@fixture.enabled=true'])
print("FILES", json.dumps([str(p.relative_to(CODEX)) for p in CODEX.rglob("*") if p.is_file() and ".system" not in str(p)]), flush=True)

def rows(case):
    result = next(r for r in RESULTS if r["case"] == case)
    assert "error" not in result["skills/list"], result
    return [s for g in result["skills/list"]["result"]["data"] for s in g["skills"]]

def plugin_rows(case, plugin_id="alpha@fixture"):
    return [s for s in rows(case) if s.get("pluginId") == plugin_id]

assert len(plugin_rows("installed")) == 2
assert not plugin_rows("disabled")
assert not plugin_rows("no-config-retained-cache")
assert not plugin_rows("override-plugins.alpha@fixture.enabled")
assert len(plugin_rows('override-plugins."alpha@fixture".enabled')) == 2
assert len(plugin_rows("override-enable")) == 2
assert all(s["enabled"] for s in plugin_rows("plugin-path-true"))
assert all("/aaa/" in s["path"] for s in plugin_rows("version-order"))
assert all("/10.0.0/" in s["path"] for s in plugin_rows("version-order-numeric"))
assert not plugin_rows("version-empty-highest")
assert all("/local/" in s["path"] for s in plugin_rows("version-local-priority"))
for case in ("in-place-skill-dir", "in-place-off"):
    item = next(s for s in rows(case) if s["name"] == "dev:p3-dev")
    assert item["enabled"] and item.get("pluginId") is None
assert all(not s["enabled"] for s in rows("automatic-path-off") if s["path"] in {str(p) for p in auto_paths})
assert plugin_rows("untrusted-project")
assert not plugin_rows("trusted-project")
assert plugin_rows("trusted-cli-override")
assert next(s for s in rows("malformed-and-cycle") if s["name"] == "no-name")["enabled"]
assert not any(s["name"] in {"no-description", "no-frontmatter"} for s in rows("malformed-and-cycle"))
assert not any("sibling" in s["name"] or "descendant" in s["name"] for s in rows("discovery-cwd"))
assert not any("parent" in s["name"] or "old-root" in s["name"] for s in rows("discovery-no-git"))
assert not any("hidden" in s["name"] for s in rows("discovery-cwd"))
assert all(r["exit"] == 0 for r in prompts)
assert "alpha:p3-" in prompts[0]["stdout"] and "alpha:p3-" not in prompts[1]["stdout"] and "alpha:p3-" in prompts[2]["stdout"]
for case, version in (("semver-prerelease", "3.0.0-alpha.10"), ("semver-release", "3.0.0"), ("semver-build", "3.0.0+10")):
    assert plugin_rows(case) and all("/" + version + "/" in s["path"] for s in plugin_rows(case))
for case in ("bundled-user-name-deny", "bundled-user-path-deny"):
    assert not next(s for s in rows(case) if s["name"] == "imagegen")["enabled"]
assert plugin_rows("remote-feature-off")
assert all(r["exit"] == 0 for r in feature_results)
assert next(line for line in feature_results[1]["stdout"].splitlines() if line.startswith("remote_plugin")).endswith("true")
assert next(line for line in feature_results[2]["stdout"].splitlines() if line.startswith("remote_plugin")).endswith("false")
write(ROOT / "assertions-passed.txt", "Core discovery, plugin identity and controls assertions passed.\n")

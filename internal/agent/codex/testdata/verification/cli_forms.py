import json, pathlib, subprocess, tempfile, sys
# Run only inside a fresh user/mount/network namespace; see README.
uid_map = pathlib.Path("/proc/self/uid_map").read_text().split()
if uid_map[0] != "0" or uid_map[2] != "1":
    raise SystemExit("Requires a mapped-root user namespace")
subprocess.run(['mount','--make-rprivate','/'],check=True)
subprocess.run(['mount','-t','tmpfs','tmpfs','/etc'],check=True)
root=pathlib.Path(tempfile.mkdtemp(prefix='skope-p3-cli-forms-',dir='/var/tmp'))
home,codex,repo=[root/n for n in ('home','codex','repo')]
for p in (home,codex,repo):p.mkdir()
env={'PATH':'/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','HOME':str(home),'USERPROFILE':str(home),'CODEX_HOME':str(codex)}
exe=sys.argv[1]
(codex/'fixture.config.toml').write_text('model = "fixture"\n')
cases=[('default',[]),('short',['-c','features.remote_plugin=false']),('short-equal',['-c=features.remote_plugin=false']),('short-attached',['-cfeatures.remote_plugin=false']),('long',['--config','features.remote_plugin=false']),('long-equal',['--config=features.remote_plugin=false']),('key-space',['-c',' features . remote_plugin = false']),('quoted-root',['-c','"features".remote_plugin=false']),('quoted-child',['-c','features."remote_plugin"=false']),('enable',['--enable','remote_plugin']),('enable-equal',['--enable=remote_plugin']),('disable',['--disable','remote_plugin']),('disable-equal',['--disable=remote_plugin']),('enable-vs-config',['--enable','remote_plugin','-c','features.remote_plugin=false']),('config-vs-enable',['-c','features.remote_plugin=false','--enable','remote_plugin'])]
cases.extend([('outer-spaces',['-c',' features.remote_plugin = false']),('root-trailing-space',['-c','features .remote_plugin=false']),('child-leading-space',['-c','features. remote_plugin=false']),('key-trailing-space',['-c','features.remote_plugin =false']),('key-leading-space',['-c',' features.remote_plugin=false'])])
rows=[]
source_cases=[('cd-short',['-C',str(repo)]),('cd-short-equal',['-C='+str(repo)]),('cd-short-attached',['-C'+str(repo)]),('cd-long',['--cd',str(repo)]),('cd-long-equal',['--cd='+str(repo)]),('profile-short',['-p','fixture']),('profile-short-equal',['-p=fixture']),('profile-short-attached',['-pfixture']),('profile-long',['--profile','fixture']),('profile-long-equal',['--profile=fixture'])]
for label,args in source_cases:
    r=subprocess.run([exe,*args,'debug','prompt-input','fixture prompt'],cwd=repo,env=env,capture_output=True,text=True,timeout=15)
    (root/(label+'.stdout')).write_text(r.stdout)
    (root/(label+'.stderr')).write_text(r.stderr)
    assert 'fixture prompt' in r.stdout, (label, 'prompt missing')
    rows.append({'case':label,'exit':r.returncode,'stderr':r.stderr if r.returncode else ''})
    assert r.returncode == 0, (label,r.stderr)
for label,args in cases:
    argv=[exe,'features','list',*args]
    r=subprocess.run(argv,cwd=repo,env=env,capture_output=True,text=True,timeout=15)
    (root/(label+'.stdout')).write_text(r.stdout)
    (root/(label+'.stderr')).write_text(r.stderr)
    feature=[line for line in r.stdout.splitlines() if line.split() and line.split()[0]=='remote_plugin']
    assert r.returncode == 0, (label,r.stderr)
    expected = label not in {'short','short-equal','short-attached','long','long-equal','disable','disable-equal','outer-spaces','key-trailing-space','key-leading-space'}
    assert len(feature) == 1 and feature[0].split()[-1] == str(expected).lower(), (label,feature)
    rows.append({'case':label,'argv':argv,'exit':r.returncode,'remote_plugin':feature,'stderr':r.stderr if r.returncode else ''})
(root/'results.json').write_text(json.dumps(rows,indent=2))
print(json.dumps({'fixture':str(root),'rows':[{k:v for k,v in row.items() if k!='argv'} for row in rows]}))
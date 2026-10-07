#!/usr/bin/env python3
"""Capture real settings in isolated profiles; no model calls or user data."""
import argparse, json, os, pathlib, shlex, subprocess, tempfile, time
p=argparse.ArgumentParser(); p.add_argument('--binary',required=True);p.add_argument('--out',required=True);p.add_argument('--version',choices=['before','after'],required=True);p.add_argument('--columns',type=int,default=120)
a=p.parse_args();out=pathlib.Path(a.out);out.mkdir(parents=True,exist_ok=True)
sock='settings-proof-'+str(os.getpid())
def tm(*args,check=True):return subprocess.run(['tmux','-L',sock,*args],text=True,capture_output=True,check=check).stdout
def key(*keys):tm('send-keys','-t','proof',*keys);time.sleep(.15)
def text(s):tm('send-keys','-t','proof','-l',s);time.sleep(.15)
def grab(name):
 time.sleep(.25)
 plain=tm('capture-pane','-p','-t','proof');paint=tm('capture-pane','-p','-e','-t','proof')
 stem=out/(name+'-'+str(a.columns)+'c');stem.with_suffix('.txt').write_text(plain);stem.with_suffix('.ansi').write_text(paint)
 if 'settings' not in plain.lower():raise RuntimeError('settings did not open: '+plain)
 print(stem,flush=True)
 return plain
with tempfile.TemporaryDirectory(prefix='codeaf-settings-proof-', ignore_cleanup_errors=True) as tmp:
 root=pathlib.Path(tmp);profile=root/'profile';profile.mkdir();work=root/'project';work.mkdir()
 cfg={'setup_seen_at':'2026-10-07T00:00:00Z','api_key':'demo-key-not-used','model.talk':'deepseek/deepseek-v4.1-flash','ui.mouse':'on'}
 for role in ('high','low','worker','reflex','mastermind'):cfg['models.tiers.'+role]='deepseek/deepseek-v4.1-flash'
 (profile/'config.json').write_text(json.dumps(cfg))
 env={'HOME':str(profile),'CODEAF_HOME':str(profile),'CODEAF_PROFILE_DIR':str(profile),'CODEAF_NO_UPDATE_CHECK':'1','CODEAF_BASE_URL':'http://127.0.0.1:9','TERM':'xterm-256color','LANG':'C.UTF-8','PATH':'/usr/bin:/bin'}
 command='cd '+shlex.quote(str(work))+' && env -i '+ ' '.join(shlex.quote(k+'='+v) for k,v in env.items())+' '+shlex.quote(a.binary)+' chat --no-host'
 try:
  tm('new-session','-d','-s','proof','-x',str(a.columns),'-y','48',command);time.sleep(2)
  text('/settings');key('Enter');time.sleep(.8)
  names=['session','context','workspace','display','spending','safety','tasks','teams','providers','connections'] if a.version=='before' else ['general','models','memory','tasks','ai-teams','permissions','spending','connections','privacy']
  for i,name in enumerate(names):
   frame=grab(f'{i+1:02}-{name}')
   if a.version=='after' and 'Advanced' in frame:
    key('Home')
    for _ in range(35):
     current=tm('capture-pane','-p','-t','proof')
     if 'Less common controls' in current:
      key('Enter');grab(f'{i+1:02}-{name}-advanced');key('End');grab(f'{i+1:02}-{name}-advanced-end');break
     key('Down')
    else:raise RuntimeError('could not reach Advanced: '+name)
   key('Right')
  for name,query in [('search-privacy','privacy'),('search-old-label','memory floor'),('search-service','github')]:
   key('C-u');text(query);grab(name)
  if a.version=='after':
   key('C-u');text('ui.hints');grab('edit-hints-before');key('Enter');grab('edit-hints-after')
   saved=json.loads((profile/'config.json').read_text())
   if saved.get('ui.hints') is not False:raise RuntimeError('hints preference did not persist')
   key('C-u');text('task.parallel');key('Enter');key('C-u');text('invalid');key('Enter');grab('edit-invalid-value')
   if json.loads((profile/'config.json').read_text()).get('task.parallel',0)!=0:raise RuntimeError('invalid draft changed saved task limit')
   key('C-u');text('4');key('Enter');grab('edit-limit-saved')
   if json.loads((profile/'config.json').read_text()).get('task.parallel')!=4:raise RuntimeError('valid task limit did not persist')
   key('Escape');key('Escape');text('/settings');key('Enter');text('task.parallel');key('Enter');grab('edit-limit-reopened')
   key('C-u');key('Enter');grab('edit-limit-cleared')
   if json.loads((profile/'config.json').read_text()).get('task.parallel')!=0:raise RuntimeError('blank task limit did not restore no limit')
   key('Enter');grab('edit-limit-unlimited');key('Escape')
  key('C-u')
  for _ in range(9):key('Left')
  # SGR mouse input reaches the real terminal event decoder.
  if a.version=='after' and a.columns>=100:
   seq='\x1b[<35;5;6M'
   tm('send-keys','-t','proof','-H',*[f'{b:02x}' for b in seq.encode()]);grab('hover-category')
  key('Escape');text('/teams');key('Enter');time.sleep(.4);grab('navigation-ai-teams')
  key('Escape');text('/history');key('Enter');time.sleep(.4);grab('navigation-activity')
 finally:
  tm('send-keys','-t','proof','C-c',check=False);time.sleep(.6);tm('kill-server',check=False);time.sleep(.3)

import pathlib,json,subprocess,re,sys
root=pathlib.Path(sys.argv[1])
for source in sorted(root.glob('*.ansi')):
 cols=int(re.search(r'-(\d+)c$',source.stem)[1]);lines=source.read_text().splitlines()
 payload='\x1b[?25l\x1b[?7l\x1b[2J\x1b[H'+''.join(f'\x1b[{i+1};1H'+line for i,line in enumerate(lines))
 cast=source.with_suffix('.cast');cast.write_text(json.dumps({'version':2,'width':cols,'height':len(lines),'env':{'TERM':'xterm-256color'}})+'\n'+json.dumps([.01,'o',payload])+'\n'+json.dumps([.1,'o',''])+'\n')
 subprocess.run(['agg','--quiet','--font-family','JetBrainsMono Nerd Font Mono','--font-size','16','--theme','dracula','--last-frame-duration','.2','--select','100%',str(cast),str(source.with_suffix('.gif'))],check=True)
 subprocess.run(['ffmpeg','-loglevel','error','-y','-i',str(source.with_suffix('.gif')),'-frames:v','1',str(source.with_suffix('.png'))],check=True)

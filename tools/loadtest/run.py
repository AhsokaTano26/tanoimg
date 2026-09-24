#!/usr/bin/env python3
"""Isolated local benchmark. Requires built native/client binaries and Docker image.
Never points at an existing data directory. Each case creates and removes its own
process/container and temporary data. Results contain no authentication secrets.
"""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import sqlite3
import subprocess as sp
import tempfile
import threading
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
p = argparse.ArgumentParser()
p.add_argument('--profiles', default='native,1g,2g')
p.add_argument('--concurrency', default='1,4,16,32,64,128')
p.add_argument('--sides', default='256,724')
p.add_argument('--seconds', type=int, default=8)
p.add_argument('--count', type=int, default=0)
p.add_argument('--repeat', type=int, default=1)
p.add_argument('--port', type=int, default=3042)
p.add_argument('--out', required=True)
p.add_argument('--convert-jpg', action='store_true')
p.add_argument('--workload', choices=['upload','gallery','download'], default='upload')
a = p.parse_args()
BASE = f'http://127.0.0.1:{a.port}'

def cmd(args, **kw):
    return sp.check_output(args, text=True, **kw).strip()

def request(client, path, data=None):
    req = urllib.request.Request(BASE + path, data=json.dumps(data).encode() if data is not None else None,
                                 headers={'Content-Type': 'application/json'})
    with client.open(req, timeout=5) as r:
        return json.load(r)

def cpu_seconds(pid):
    value = cmd(['ps', '-o', 'time=', '-p', str(pid)]).strip()
    parts = value.split(':')
    return sum(float(x) * 60**i for i, x in enumerate(reversed(parts)))

def container_metrics(name):
    raw = cmd(['docker','exec',name,'sh','-c',
        'cat /proc/1/status; echo CGROUP; cat /sys/fs/cgroup/memory.peak /sys/fs/cgroup/memory.current; echo EVENTS; cat /sys/fs/cgroup/memory.events; echo CPU; cat /sys/fs/cgroup/cpu.stat; echo STAT; cat /sys/fs/cgroup/memory.stat'])
    proc, rest = raw.split('CGROUP\n')
    mem, rest = rest.split('EVENTS\n')
    events, rest = rest.split('CPU\n')
    cpu, stat = rest.split('STAT\n')
    pairs = lambda text: {k: int(v) for k,v in (line.split() for line in text.splitlines())}
    vmhwm = next(int(line.split()[1]) for line in proc.splitlines() if line.startswith('VmHWM:'))
    return {'server_peak_rss_mib':vmhwm/1024, 'cgroup_peak_mib':int(mem.splitlines()[0])/2**20,
            'cgroup_current_mib':int(mem.splitlines()[1])/2**20, 'memory_events':pairs(events),
            'cpu':pairs(cpu), 'memory_stat':pairs(stat)}

def run_case(profile, side, concurrency, rep):
    name = 'tanoimg-perf-' + secrets.token_hex(5)
    env = dict(os.environ, TANOIMG_ADMIN_PASSWORD=secrets.token_urlsafe(24), TANOIMG_PUBLIC_URL='', TANOIMG_ADMIN_USER='admin')
    container = profile != 'native'
    server = None
    finished = threading.Event()
    rss = []
    with tempfile.TemporaryDirectory(prefix='tanoimg-perf-') as temp:
        data = Path(temp)/'data'
        log = open(Path(temp)/'server.log','w+')
        try:
            if container:
                cmd(['docker','run','-d','--name',name,'--memory',profile,'--memory-swap',profile,'--cpus','2',
                     '-p',f'127.0.0.1:{a.port}:3000','--mount','type=volume,destination=/data',
                     '-e','TANOIMG_ADMIN_PASSWORD','tanoimg-perf:local'],env=env)
            else:
                server = sp.Popen([str(ROOT/'tanoimg'),'serve','-data',str(data),'-addr',f'127.0.0.1:{a.port}'],env=env,stdout=log,stderr=log)
            client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
            for _ in range(100):
                try:
                    request(client,'/api/settings/public');break
                except Exception:
                    time.sleep(.1)
            else: raise RuntimeError('server did not become ready')
            login=request(client,'/api/auth/login',{'username':'admin','password':env['TANOIMG_ADMIN_PASSWORD']})
            assert login['success'], 'login failed'
            key=request(client,'/api/apikeys',{'name':'disposable-load-test'})['data']['key']
            if a.convert_jpg:
                req=urllib.request.Request(BASE+'/api/config/private', data=json.dumps({'convertToJpg':True}).encode(),method='PUT',headers={'Content-Type':'application/json'})
                with client.open(req) as r: assert json.load(r)['success']
            seed_count=0
            endpoint=BASE+'/api/upload/private'
            if a.workload!='upload':
                seed_count=1000
                seed=json.loads(cmd(['/tmp/tanoimg-loadtest','-url',endpoint+'?visibility=public','-side',str(side),'-count',str(seed_count),'-concurrency','4'],env=dict(os.environ,TANOIMG_BENCH_KEY=key)))
                assert seed['success']==seed_count
                gallery=request(client,'/api/images?scope=public&limit=50')['data']
                assert gallery['pagination']['total']==seed_count
                endpoint=BASE+'/api/images?scope=public&limit=50' if a.workload=='gallery' else BASE+gallery['images'][0]['url']
            if container:
                before=container_metrics(name)
            else:
                before=cpu_seconds(server.pid)
                def monitor():
                    while not finished.is_set():
                        try: rss.append(int(cmd(['ps','-o','rss=','-p',str(server.pid)]))/1024)
                        except Exception: pass
                        finished.wait(.1)
                thread=threading.Thread(target=monitor);thread.start()
            started=time.monotonic()
            args=['/tmp/tanoimg-loadtest','-url',endpoint,'-side',str(side),'-concurrency',str(concurrency),'-duration',str(a.seconds)+'s']
            if a.workload!='upload': args+=['-get']
            if a.count: args+=['-count',str(a.count)]
            result=json.loads(cmd(args,env=dict(os.environ,TANOIMG_BENCH_KEY=key)))
            wall=time.monotonic()-started
            if container:
                metrics=container_metrics(name)
                cpu=(metrics['cpu']['usage_usec']-before['cpu']['usage_usec'])/1e6
                result.update(metrics)
            else:
                cpu=cpu_seconds(server.pid)-before
                finished.set();thread.join()
                result['server_peak_rss_mib']=max(rss,default=0)
            result.update(profile=profile,side=side,repeat=rep,convert_jpg=a.convert_jpg,workload=a.workload,
                          server_cpu_seconds=cpu,server_cpu_cores=cpu/wall,measurement_wall_seconds=wall,
                          cpu_limit=2 if container else 14)
            # Stop before copying SQLite so there are no concurrent writers. Preserve WAL too.
            if container:
                cmd(['docker','stop','-t','2',name])
                state=json.loads(cmd(['docker','inspect','--format','{{json .State}}',name]))
                result['oom_killed']=state['OOMKilled']
                data.mkdir()
                cmd(['docker','cp',name+':/data/tanoimg.db',str(data/'tanoimg.db')])
                for suffix in ('-wal','-shm'):
                    sp.run(['docker','cp',name+':/data/tanoimg.db'+suffix,str(data/('tanoimg.db'+suffix))],stdout=sp.DEVNULL,stderr=sp.DEVNULL)
                # Count actual files without copying gigabytes back to host.
                # Database is validated below; per-file count applies to native cases.
            else:
                server.terminate();server.wait(timeout=10)
                result['stored_files']=len(list((data/'uploads').glob('*')))
            with sqlite3.connect(data/'tanoimg.db') as db:
                result['stored_images']=db.execute('select count(*) from images').fetchone()[0]
                result['integrity']=db.execute('pragma quick_check').fetchone()[0]
            expected=result['success'] if a.workload=='upload' else seed_count
            result['stored_minus_acknowledged']=result['stored_images']-expected
            result['acknowledged_consistent']=result['stored_images']==expected
            if not container: assert result['stored_files']==result['stored_images'], 'stored files mismatch'
            assert result['integrity']=='ok'
            with open(a.out,'a') as out:out.write(json.dumps(result)+'\n')
            print(json.dumps({k:result[k] for k in ['profile','side','concurrency','success_rps','error_pct','p95_ms','server_peak_rss_mib','stored_minus_acknowledged','statuses']}),flush=True)
        finally:
            finished.set()
            if server and server.poll() is None: server.terminate();server.wait(timeout=10)
            if container:sp.run(['docker','rm','-f','-v',name],stdout=sp.DEVNULL,stderr=sp.DEVNULL)
            log.close()

for profile in a.profiles.split(','):
    for side in map(int,a.sides.split(',')):
        for concurrency in map(int,a.concurrency.split(',')):
            for rep in range(a.repeat):
                run_case(profile,side,concurrency,rep)

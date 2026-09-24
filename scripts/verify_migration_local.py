#!/usr/bin/env python3
"""Run the real migration client against a disposable local server and verify data.
Never starts the imported settings' notification or moderation workers.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import secrets
import sqlite3
import subprocess
import tempfile
import time
import urllib.request

spec=importlib.util.spec_from_file_location('migration',Path(__file__).with_name('migrate_easyimg.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
p=argparse.ArgumentParser();p.add_argument('--source',required=True);p.add_argument('--binary',required=True);p.add_argument('--port',type=int,default=3044);args=p.parse_args()
source=Path(args.source).resolve();audit=m.audit_source(source)
with tempfile.TemporaryDirectory(prefix='tanoimg-migration-check-') as temp:
    data=Path(temp)/'data';log=open(Path(temp)/'server.log','w')
    password=secrets.token_urlsafe(24)
    env=dict(os.environ,TANOIMG_ADMIN_USER='admin',TANOIMG_ADMIN_PASSWORD=password,TANOIMG_PUBLIC_URL='',TANOIMG_MIGRATION_PASSWORD=password)
    server=subprocess.Popen([args.binary,'serve','-data',str(data),'-addr',f'127.0.0.1:{args.port}'],env=env,stdout=log,stderr=log)
    try:
        for _ in range(100):
            try:
                with urllib.request.urlopen(f'http://127.0.0.1:{args.port}/healthz',timeout=1) as r:
                    assert json.load(r)['success']
                break
            except Exception:time.sleep(.1)
        else:raise RuntimeError('server failed to start')
        subprocess.run(['python3',str(Path(__file__).with_name('migrate_easyimg.py')),'--source',str(source),'--url',f'http://127.0.0.1:{args.port}'],env=env,check=True)
        state=json.loads((source/'.tanoimg-migration/archive.json').read_text())
        ready=data/'imports'/state['sha256']/'ready'
        with sqlite3.connect(ready/'tanoimg.db') as db:
            images=db.execute('select id,uuid,filename,size,is_deleted from images').fetchall()
            assert len(images)==len(audit['documents']['images'])
            for identity,uuid,filename,size,deleted in images:
                old=audit['documents']['images'][identity]
                assert (uuid,filename,size,bool(deleted))==(old['uuid'],old['filename'],old['size'],bool(old.get('isDeleted')))
                assert m.sha256_file(ready/'uploads'/filename)==m.sha256_file(source/'uploads'/filename)
            users=db.execute('select id,username,password from users').fetchall()
            assert len(users)==len(audit['documents']['users'])
            for identity,username,password_hash in users:
                old=audit['documents']['users'][identity]
                assert (username,password_hash)==(old['username'],old['password'])
            keys=db.execute('select id,key from apikeys').fetchall()
            assert len(keys)==len(audit['documents']['apikeys'])
            for identity,key in keys:assert key==audit['documents']['apikeys'][identity]['key']
            for old in audit['documents']['settings'].values():
                value=db.execute('select value from settings where key=?',(old['key'],)).fetchone()[0]
                assert json.loads(value)==old['value']
            assert db.execute('pragma quick_check').fetchone()[0]=='ok'
        with sqlite3.connect(data/'tanoimg.db') as db:
            assert db.execute('select count(*) from images').fetchone()[0]==0
            assert db.execute('select username from users').fetchone()[0]=='admin'
        print(json.dumps({'verified_images':len(images),'verified_users':len(users),'verified_api_keys':len(keys),'ids_and_paths_preserved':True,'all_image_sha256_match':True,'live_database_untouched':True}))
    finally:
        server.terminate();server.wait(timeout=10);log.close()

#!/usr/bin/env python3
"""Preserve EasyImg IDs and /i/<filename> paths using TanoImg's migration API.
Python 3 standard library only. Credentials are never stored in the archive cache.
"""
import argparse
import getpass
import hashlib
import http.client
import http.cookiejar
import io
import ipaddress
import json
import os
from pathlib import Path
import re
import socket
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

KINDS = ('users','apikeys','settings','images','moderation_tasks','ip_blacklist')
FILENAME = re.compile(r'^[a-fA-F0-9-]{32,36}\.[a-zA-Z0-9]+$')
CHUNK = 8 << 20


def sha256_file(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1 << 20), b''): h.update(block)
    return h.hexdigest()


def audit_source(root):
    root = Path(root)
    if any((root/d).is_symlink() or not (root/d).is_dir() for d in ('db','uploads')):
        raise ValueError('源目录必须包含真实的 db/ 和 uploads/ 目录，不能是符号链接')
    documents = {}
    fingerprint = hashlib.sha256()
    for path in (root/'db').glob('*.db'):
        if path.stem not in KINDS: raise ValueError('发现尚不支持的数据库文件：'+path.name)
    for kind in KINDS:
        path = root/'db'/(kind+'.db')
        if path.is_symlink(): raise ValueError('数据库不能是符号链接')
        docs = {}
        if path.exists():
            before = path.stat()
            digest = hashlib.sha256()
            with path.open('rb') as f:
                for number, line in enumerate(f, 1):
                    digest.update(line)
                    try: doc = json.loads(line)
                    except (ValueError, UnicodeError): raise ValueError(f'{path.name}:{number} 不是有效 JSON') from None
                    if not isinstance(doc, dict): raise ValueError('NeDB 文档必须是对象')
                    if '$$indexCreated' in doc or '$$indexRemoved' in doc: continue
                    identity = doc.get('_id')
                    if not isinstance(identity, str) or not identity: raise ValueError('NeDB 文档缺少字符串 _id')
                    if doc.get('$$deleted'): docs.pop(identity, None)
                    else: docs[identity] = doc
            after = path.stat()
            if (before.st_size,before.st_mtime_ns)!=(after.st_size,after.st_mtime_ns):
                raise ValueError('检查期间数据库发生变化，请先停止 EasyImg 写入')
            fingerprint.update((kind+':'+digest.hexdigest()+'\n').encode())
        elif kind in ('images','users'): raise ValueError('缺少 '+kind+'.db')
        documents[kind] = docs
    if not documents['users'] or any(not d.get('username') or not d.get('password') for d in documents['users'].values()):
        raise ValueError('备份必须包含有效的管理员账户')
    files = []
    sizes = {}
    for path in sorted((root/'uploads').iterdir()):
        if path.name == '.DS_Store': continue
        if path.is_symlink() or not path.is_file() or not FILENAME.fullmatch(path.name):
            raise ValueError('uploads 中存在非图片文件、目录或符号链接：'+path.name)
        st = path.stat()
        files.append((path.name,st.st_size,st.st_mtime_ns))
        sizes[path.name] = st.st_size
        fingerprint.update(f'{path.name}:{st.st_size}:{st.st_mtime_ns}\n'.encode())
    references = set()
    for doc in documents['images'].values():
        name, uuid = doc.get('filename',''), doc.get('uuid','')
        if not isinstance(name,str) or not isinstance(uuid,str) or not uuid or not FILENAME.fullmatch(name) or not name.startswith(uuid+'.'):
            raise ValueError('图片记录的 uuid/filename 不一致')
        if name not in sizes: raise ValueError('缺少原图：'+name)
        if doc.get('size') != sizes[name]: raise ValueError('原图大小与数据库不一致：'+name)
        if name in references: raise ValueError('多条图片记录引用同一文件：'+name)
        references.add(name)
    summary = {kind:len(docs) for kind,docs in documents.items()}
    summary.update(files=len(files),bytes=sum(size for _,size,_ in files),
                   deletedImages=sum(bool(d.get('isDeleted')) for d in documents['images'].values()),
                   unreferencedFiles=len(sizes.keys()-references))
    return {'documents':documents,'files':files,'fingerprint':fingerprint.hexdigest(),'summary':summary}


def atomic_json(path, data):
    fd, name = tempfile.mkstemp(prefix='.state-',dir=path.parent)
    try:
        with os.fdopen(fd,'w') as f:
            json.dump(data,f); f.flush(); os.fsync(f.fileno())
        os.replace(name,path)
    finally:
        if os.path.exists(name): os.unlink(name)


def prepare_archive(root, cache, audit):
    root,cache = Path(root),Path(cache)
    cache.mkdir(parents=True,exist_ok=True,mode=0o700)
    os.chmod(cache,0o700)
    archive,receipt = cache/'source.tar',cache/'archive.json'
    if receipt.exists() and archive.exists():
        old = json.loads(receipt.read_text())
        if old.get('sourceFingerprint') == audit['fingerprint'] and old.get('size') == archive.stat().st_size and old.get('sha256') == sha256_file(archive):
            return old
    fd,name = tempfile.mkstemp(prefix='.archive-',dir=cache)
    try:
        with os.fdopen(fd,'wb') as f:
            with tarfile.open(fileobj=f,mode='w|',format=tarfile.USTAR_FORMAT) as tar:
                for kind in KINDS:
                    docs = audit['documents'][kind]
                    data = b''.join((json.dumps(docs[key],ensure_ascii=False,sort_keys=True,separators=(',',':'))+'\n').encode() for key in sorted(docs))
                    info = tarfile.TarInfo('db/'+kind+'.db');info.mode=0o600;info.size=len(data)
                    tar.addfile(info,io.BytesIO(data))
                for filename,size,mtime in audit['files']:
                    path = root/'uploads'/filename
                    with path.open('rb') as source:
                        st=os.fstat(source.fileno())
                        if path.is_symlink() or (st.st_size,st.st_mtime_ns)!=(size,mtime): raise ValueError('打包期间原图发生变化')
                        info=tarfile.TarInfo('uploads/'+filename);info.mode=0o600;info.size=size
                        tar.addfile(info,source)
            f.flush();os.fsync(f.fileno())
        if audit_source(root)['fingerprint'] != audit['fingerprint']: raise ValueError('打包期间备份发生变化，请重试')
        os.replace(name,archive)
        state={'sha256':sha256_file(archive),'size':archive.stat().st_size,'sourceFingerprint':audit['fingerprint'],'summary':audit['summary']}
        atomic_json(receipt,state)
        return state
    finally:
        if os.path.exists(name): os.unlink(name)


def validate_url(url):
    parsed=urllib.parse.urlsplit(url)
    try: local=parsed.hostname=='localhost' or ipaddress.ip_address(parsed.hostname or '').is_loopback
    except ValueError: local=False
    if not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment or (parsed.scheme!='https' and not (parsed.scheme=='http' and local)):
        raise ValueError('远端必须使用 HTTPS；仅本机测试允许 HTTP。URL 不能包含凭证、查询参数或 fragment')
    return url.rstrip('/')


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs): return None


class RemoteError(Exception):
    def __init__(self,code,message): super().__init__(message);self.code=code


class Remote:
    def __init__(self,url):
        self.url=validate_url(url)
        self.client=urllib.request.build_opener(NoRedirect(),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def request(self,method,path,data=None):
        body=data if isinstance(data,bytes) else json.dumps(data).encode() if data is not None else None
        req=urllib.request.Request(self.url+path,data=body,method=method,headers={'Content-Type':'application/octet-stream' if isinstance(data,bytes) else 'application/json'})
        try:
            with self.client.open(req,timeout=120) as response: raw=response.read(1<<20)
        except urllib.error.HTTPError as error:
            try: message=json.loads(error.read(65536)).get('message','请求失败')
            except (ValueError,AttributeError): message='请求失败'
            if error.code in (404,405): message+='；请确认远端已更新到包含迁移 API 的镜像'
            if error.code==401: message+='；重新运行脚本登录可继续上传'
            raise RemoteError(error.code,f'HTTP {error.code}: {message}') from None
        except (urllib.error.URLError,TimeoutError,socket.timeout,ConnectionError,OSError,http.client.HTTPException) as error:
            raise RemoteError(0,'连接中断或超时') from error
        try: result=json.loads(raw)
        except ValueError: raise RemoteError(0,'远端未返回 JSON，请检查代理/域名') from None
        if not result.get('success'): raise RemoteError(400,result.get('message','远端请求失败'))
        return result.get('data')
    def retry(self,method,path,data=None):
        for attempt in range(6):
            try: return self.request(method,path,data)
            except RemoteError as error:
                if error.code not in (0,408,429,500,502,503,504) or attempt==5: raise
                time.sleep(min(2**attempt,15))
    def login(self,username):
        password=os.environ.get('TANOIMG_MIGRATION_PASSWORD') or getpass.getpass('当前远端管理员密码：')
        auth=self.request('POST','/api/auth/login',{'username':username,'password':password})
        if auth.get('requiresTOTP'):
            code=getpass.getpass('TOTP 动态码或一次性恢复码：')
            self.request('POST','/api/auth/totp',{'challenge':auth['challenge'],'code':code})
        capabilities=self.request('GET','/api/admin/migrations')
        if capabilities.get('protocol')!=1: raise ValueError('远端迁移协议不兼容')
    def upload(self,archive,state,restart=False):
        path='/api/admin/migrations/'+state['sha256']
        status=self.retry('POST','/api/admin/migrations',{'sha256':state['sha256'],'size':state['size']})
        if restart:
            self.retry('DELETE',path)
            status=self.retry('POST','/api/admin/migrations',{'sha256':state['sha256'],'size':state['size']})
        failures=0;last_print=0
        with archive.open('rb') as f:
            while status['offset']<state['size']:
                offset=status['offset']
                if offset<0 or offset>state['size']: raise ValueError('远端偏移量无效')
                f.seek(offset);block=f.read(min(CHUNK,status['chunkSize']))
                try:
                    status=self.request('PUT',path+'/archive?offset='+str(offset),block)
                    failures=0
                except RemoteError as error:
                    if error.code not in (0,408,409,429,500,502,503,504) or failures>=6: raise
                    failures+=1;time.sleep(min(2**failures,15))
                    status=self.retry('GET',path)
                if time.monotonic()-last_print>1:
                    print(f"上传 {100*status['offset']/state['size']:.1f}% ({status['offset']/2**20:.1f}/{state['size']/2**20:.1f} MiB)",flush=True)
                    last_print=time.monotonic()
        if status['phase']!='ready':
            status=self.retry('POST',path+'/finalize')
            print('远端正在校验并转换数据库；中断本地脚本不会取消远端准备。',flush=True)
        while status['phase'] not in ('ready','failed'):
            time.sleep(2);status=self.retry('GET',path)
        if status['phase']!='ready': raise ValueError('远端准备失败：'+status.get('error','未知错误')+'。修复原因后重试，归档损坏可加 --restart-upload 重新上传。')
        return status


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source',default='tmp',help='包含 db/ 和 uploads/ 的 EasyImg 备份')
    parser.add_argument('--url',help='远端 TanoImg HTTPS 地址')
    parser.add_argument('--username',default='admin',help='当前远端管理员（不是备份中的旧账户）')
    parser.add_argument('--cache-dir',help='归档缓存目录，默认 <source>/.tanoimg-migration')
    parser.add_argument('--dry-run',action='store_true',help='仅检查源备份，不联系远端')
    parser.add_argument('--prepare-only',action='store_true',help='仅检查并生成可续传归档')
    parser.add_argument('--restart-upload',action='store_true',help='清除远端未完成的暂存数据后重新上传，不允许删除已准备好的数据')
    args=parser.parse_args()
    if not args.url and not (args.dry_run or args.prepare_only): parser.error('请提供 --url')
    root=Path(args.source).resolve()
    audit=audit_source(root)
    print('备份检查通过：'+json.dumps(audit['summary'],ensure_ascii=False),flush=True)
    if args.dry_run:return
    remote=None
    if not args.prepare_only:
        remote=Remote(args.url);remote.login(args.username)
    cache=Path(args.cache_dir).resolve() if args.cache_dir else root/'.tanoimg-migration'
    print('正在准备/校验本地归档缓存……',flush=True)
    state=prepare_archive(root,cache,audit)
    if args.prepare_only:
        print(f"归档已准备：{cache/'source.tar'}，{state['size']/2**20:.1f} MiB");return
    result=remote.upload(cache/'source.tar',state,args.restart_upload)
    print('远端迁移准备完成，报告：'+json.dumps(result['report'],ensure_ascii=False))
    print('\n原图库尚未切换。请在 Zeabur 设置以下环境变量，然后重新部署：')
    print('TANOIMG_DATA='+result['dataDir'])
    print('保持 /data 持久卷挂载；切换后使用原 EasyImg 账户登录，/i/ 原文件路径不变。')


if __name__=='__main__':
    try: main()
    except KeyboardInterrupt:
        print('\n已停止本地传输；重新执行相同命令可续传。',file=sys.stderr);sys.exit(130)
    except (ValueError,OSError,RemoteError) as error:
        print('错误：'+str(error),file=sys.stderr);sys.exit(1)

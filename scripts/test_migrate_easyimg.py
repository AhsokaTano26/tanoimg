import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('migrate_easyimg',Path(__file__).with_name('migrate_easyimg.py'))
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class MigrationClientTests(unittest.TestCase):
    def test_compaction_and_missing_files(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);(root/'db').mkdir();(root/'uploads').mkdir()
            name='11111111-1111-1111-1111-111111111111.png'
            doc={'_id':'old-id','uuid':name[:-4],'filename':name,'size':3,'isDeleted':True}
            (root/'db/images.db').write_text(json.dumps(dict(doc,size=2))+'\n'+json.dumps(doc)+'\n'+json.dumps({'_id':'removed','filename':'bad'})+'\n'+json.dumps({'_id':'removed','$$deleted':True})+'\n')
            (root/'db/users.db').write_text(json.dumps({'_id':'u','username':'old','password':'hash'})+'\n')
            (root/'uploads'/name).write_bytes(b'abc')
            audit=m.audit_source(root)
            self.assertEqual(audit['summary']['images'],1)
            self.assertEqual(audit['summary']['deletedImages'],1)
            self.assertEqual(audit['documents']['images']['old-id']['size'],3)
            cache=root/'cache'
            first=m.prepare_archive(root,cache,audit)
            second=m.prepare_archive(root,cache,audit)
            self.assertEqual(first['sha256'],second['sha256'])
            import tarfile
            with tarfile.open(cache/'source.tar') as tar:
                self.assertEqual(tar.extractfile('uploads/'+name).read(),b'abc')
                self.assertEqual(json.loads(tar.extractfile('db/images.db').read())['_id'],'old-id')
            (root/'uploads'/name).unlink()
            with self.assertRaises(ValueError):m.audit_source(root)

    def test_reject_symlinks(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);(root/'db').mkdir();(root/'uploads').mkdir()
            (root/'db/images.db').symlink_to('/etc/hosts')
            with self.assertRaises(ValueError):m.audit_source(root)

    def test_reject_insecure_remote(self):
        with self.assertRaises(ValueError):m.validate_url('http://example.com')
        with self.assertRaises(ValueError):m.validate_url('https://user:password@example.com')
        self.assertEqual(m.validate_url('https://example.com/'),'https://example.com')
        self.assertEqual(m.validate_url('http://127.0.0.1:3044'),'http://127.0.0.1:3044')

if __name__=='__main__':unittest.main()

#!/usr/bin/env python3
"""Summarize recorded measurements, retaining failed and overloaded cases."""
import csv
import json
from pathlib import Path
import sys

root=Path(sys.argv[1] if len(sys.argv)>1 else 'docs/benchmarks')
fields=['source','profile','workload','convert_jpg','convert_webp','side','concurrency','success','requests','seconds','success_rps','upload_mib_s','response_mib_s','p50_ms','p95_ms','p99_ms','error_pct','server_peak_rss_mib','cgroup_peak_mib','server_cpu_cores','estimated_10000_seconds','stored_minus_acknowledged','files_minus_rows','oom_killed']
with (root/'2026-09-25-summary.csv').open('w') as out:
 writer=csv.DictWriter(out,fields);writer.writeheader()
 for path in sorted(root.glob('2026-09-25-*.jsonl')):
  for line in path.read_text().splitlines():
   r=json.loads(line)
   assert sum(r['statuses'].values())==r['requests']
   assert r['statuses'].get('200',0)==r['success']
   assert abs(r['success']/r['seconds']-r['success_rps'])<.001
   assert r['integrity']=='ok'
   r['source']=path.name
   r.setdefault('workload','upload')
   r.setdefault('stored_minus_acknowledged',r['stored_images']-(r['success'] if r['workload']=='upload' else 1000))
   r['estimated_10000_seconds']=10000/r['success_rps'] if r['success_rps'] else ''
   writer.writerow({k:r.get(k,'') for k in fields})
print('Validated and summarized raw measurements.')

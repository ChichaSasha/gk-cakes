# Planning verification — r5

Performed 2026-09-21 against the frozen review manifest. Planning-kit checker
self-test and default checker passed. The additional check below passed for
all design links/whitespace, 40 exact requirement rows, 34 templates, ordered
acyclic dependencies and truthful publication lanes. Initial input hashes remain
unchanged except the authorized SETUP.md update. An initial ad-hoc coverage regex
expected a bare ID cell; it failed on the actual ID-plus-description format and
was corrected to check those rows. No missing requirement was concealed.

From repository root, run `python3 scripts/check_planning_kit.py --self-test` and
`python3 scripts/check_planning_kit.py`. Then run this with Python 3:

```python
from pathlib import Path
import hashlib,json,re,runpy
root=Path.cwd(); check=runpy.run_path(str(root/'scripts/check_planning_kit.py'))['check_document'];errors=[]
for p in (root/'docs/design').rglob('*.md'):errors += [str(p)+': '+e for e in check(p)]
assert not errors,errors
rows=json.loads((root/'docs/design/ticket-plan.json').read_text()); by={x['id']:x for x in rows};assert len(rows)==len(by)==34
for x in rows:
 for dep in x['depends']:assert dep in by and by[dep]['wave']<x['wave'] and by[dep]['order']<x['order'],(x['id'],dep)
 if x.get('parallel_group'):
  assert f"Parallel group: {x['parallel_group']}" in (root/'docs/design/tickets'/(x['id']+'.md')).read_text()
 assert not any(k in x for k in ['rule','acs','block','contract']), 'descriptions must have one maintained source'
 s=(root/'docs/design/tickets'/ (x['id']+'.md')).read_text()
 assert s.splitlines()[0]==f"# [{x['id']}][{x['priority']}][O{x['order']:03}][W{x['wave']:02}] {x['title']}"
 assert f"Order: {x['order']}; Wave: {x['wave']};" in s
 deps=re.search(r'^Depends on: ([^;]+);',s,re.M)[1]
 assert deps==(', '.join(x['depends']) or 'none')
 for heading in ['## Context','## To dos','## Acceptance Criteria']:assert heading in s,(x['id'],heading)
 assert all('AC'+str(i)+'.' in s for i in range(1,5)),x['id']
 assert x['publication_list']==('Ready' if x['wave']==0 and x['owner']=='Creator' else 'Backlog')
 assert x['blocked']==(x['publication_list']=='Backlog')
# Bootstrap has B1/G1/B2, not a completed-ticket cycle.
start=lambda k:'GK-020-B1' if k=='GK-020' else k
finish=lambda k:'GK-020-B2' if k=='GK-020' else k
edges=[(start(d) if x['id']=='GK-013' and d=='GK-020' else finish(d),start(x['id'])) for x in rows for d in x['depends']]+[('GK-013','GK-020-B2')]
nodes={start(x['id']) for x in rows}|{'GK-020-B2'};waves={}
while len(waves)<len(nodes):
 ready=[n for n in nodes-waves.keys() if all(a in waves for a,b in edges if b==n)]
 assert ready,'milestone cycle'
 for n in ready:waves[n]=max([waves[a]+1 for a,b in edges if b==n] or [0])
assert all(x['wave']==waves[start(x['id'])] for x in rows)
def ancestors(n):
 result=set();pending=[a for a,b in edges if b==n]
 while pending:
  a=pending.pop()
  if a not in result:result.add(a);pending.extend(p for p,b in edges if b==a)
 return result
for x in rows:
 for parallel in x.get('parallel',[]):
  assert x['id'] in by[parallel].get('parallel',[]),(x['id'],parallel,'not reciprocal')
  assert finish(parallel) not in ancestors(start(x['id'])) and finish(x['id']) not in ancestors(start(parallel)),(x['id'],parallel,'dependent parallel pair')
  text=(root/'docs/design/tickets'/(x['id']+'.md')).read_text()
  assert parallel in re.search(r'^Safe in parallel with: (.*)$',text,re.M)[1]

assert all('GK-021' in by[k]['depends'] for k in ['GK-022','GK-023'])
assert 'GK-027' in by['GK-029']['depends']
assert by['GK-020']['completion_after']==['GK-013']
assert 'GK-020' in by['GK-013']['dependency_deliverables']
brief=(root/'docs/planning/PROJECT-BRIEF.md').read_text();coverage=(root/'docs/design/coverage.md').read_text()
ids=set(re.findall(r'\b(?:C\d{2}|A\d{2}|S\d{2}|D\d{2}|UX\d{2})\b',brief));assert len(ids)==40,len(ids)
assert set(re.findall(r'^\| ((?:C|A|S|D|UX)\d{2}) —',coverage,re.M))==ids
initial=json.loads((root/'docs/design/input-revision.json').read_text())
for name,digest in initial['sha256'].items():
 if name!='SETUP.md':assert hashlib.sha256((root/name).read_bytes()).hexdigest()==digest,name
print('PASS: design links/whitespace; 40 requirement rows; 34 complete templates; acyclic ordered dependencies; 7 Creator Ready/27 blocked Backlog; initial sources preserved except authorized SETUP update.')
```

Actual browser checks and limitations are recorded in [sketch session](sessions/visual-foundation/session.md). No application, PostgreSQL concurrency, provider sandbox, delivery pipeline or restore tests ran. Those remain future ticket acceptance checks.

r4 extends checks to exact metadata/title/order/dependency agreement, absent duplicated
rule/AC prose, explicit B1/G1/B2 milestone acyclicity and known auth/reorder
dependencies. Semantic review remains necessary; structural checks did not discover
the original missing prerequisites or image preservation gap.

r5 adds parallel-group equality, reciprocal pairing and transitive milestone
ancestry checks. The selected-image boundary still requires semantic review and
the future view-A/replace-B/freeze regression; no application test is claimed.

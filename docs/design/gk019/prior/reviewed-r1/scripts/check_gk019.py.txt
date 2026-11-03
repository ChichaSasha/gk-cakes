#!/usr/bin/env python3
"""Validate GK-019 design artifacts, not application or provider behavior."""
import hashlib
import json
import re
from pathlib import Path
from check_planning_kit import check_document

ROOT = Path(__file__).resolve().parents[1]
PACKET = ROOT / 'docs/design/gk019'


def read(path):
    return json.loads(path.read_text())


def main():
    errors = []
    for path in [ROOT / 'SETUP.md', ROOT / 'docs/planning/DELIVERY.md']:
        errors.extend(f'{path.relative_to(ROOT)}: {x}' for x in check_document(path))
    for folder in ('docs/design', 'docs/creator'):
        for path in (ROOT / folder).rglob('*.md'):
            # Frozen copied evidence keeps source-relative links; validate maintained documents.
            if any(part.startswith('evidence-') for part in path.relative_to(ROOT).parts):
                continue
            errors.extend(f'{path.relative_to(ROOT)}: {x}' for x in check_document(path))
    assert not errors, '\n'.join(errors)
    brief = (ROOT / 'docs/planning/PROJECT-BRIEF.md').read_text()
    ids = set(re.findall(r'\b(?:C|A|S|D|UX)\d{2}\b', brief))
    coverage = (ROOT / 'docs/design/coverage.md').read_text()
    rows = re.findall(r'^\| ((?:C|A|S|D|UX)\d{2}) —', coverage, re.M)
    assert len(rows) == len(set(rows)) == len(ids) == 40 and set(rows) == ids

    # Preserve published dependency graph and verify current deferred gates separately.
    tickets = read(ROOT / 'docs/design/ticket-plan.json')
    by = {t['id']: t for t in tickets}
    assert len(by) == len(tickets)
    edges = [(d, t['id']) for t in tickets for d in t['depends']]
    assert all(a in by and b in by for a, b in edges)
    done = set()
    while len(done) < len(by):
        ready = {n for n in by.keys() - done if all(a in done for a, b in edges if b == n)}
        assert ready, 'Dependency cycle'
        done |= ready
    # Validate actual B1 -> gate -> B2 milestone, not a completed-ticket cycle.
    start = lambda n: 'GK-020-B1' if n == 'GK-020' else n
    finish = lambda n: 'GK-020-B2' if n == 'GK-020' else n
    milestones = [(start(a) if b == 'GK-013' and a == 'GK-020' else finish(a), start(b)) for a, b in edges]
    overlay = read(PACKET / 'dependency-overlay.json')
    milestones += [(finish(a), start(b)) for a, b in overlay['additional_edges']]
    milestones += [tuple(edge) for edge in overlay['milestone_edges']]
    assert set(overlay['new_nodes']) == {'GK-037', 'GK-038'}
    assert all(a in by or a in overlay['new_nodes'] for edge in overlay['additional_edges'] for a in edge)
    nodes = {n for edge in milestones for n in edge}
    done = set()
    while done != nodes:
        ready = {n for n in nodes - done if all(a in done for a, b in milestones if b == n)}
        assert ready, 'Milestone cycle'
        done |= ready

    schema = read(PACKET / 'schema.json')
    names = {r['name'] for r in schema['relations']}
    assert len(names) == len(schema['relations'])
    known = names | set(schema['unexpanded_existing_catalog_relations'])
    for relation in schema['relations']:
        assert relation['owner'] and relation['columns'] and relation['constraints']
        for field in relation['columns'].values():
            for foreign in re.findall(r'\bFK (\w+)', field):
                assert foreign in known, (relation['name'], foreign)

    cases = read(PACKET / 'cases.json')
    expected = ['Cards', 'Cartes Bancaires', 'Apple Pay', 'Amazon Pay', 'Kakao Pay',
                'MB WAY', 'Naver Pay', 'Onelink', 'PAYCO', 'Revolut Pay', 'Samsung Pay',
                'Satispay', 'Bancontact', 'BLIK', 'EPS', 'Klarna', 'Pix']
    assert cases['accepted_method_labels'] == expected
    assert cases['disabled_method_labels'] == ['Google Pay']
    for name in expected:
        assert f'| {name} |' in (PACKET / 'payment-methods.md').read_text()
    assert cases['sender'] == {'name': 'GK Cakes', 'from': 'orders@gkcake.com',
                               'reply_to': 'gk.cake.london@gmail.com'}
    for case in cases['refund_cases']:
        target = (sum(case['paid']) * case['rate'] + 50) // 100
        assert target == case['target'] == sum(case['allocations']), case['id']
        assert all(0 <= a <= p for a, p in zip(case['allocations'], case['paid']))
    for case in cases['prior_refund_cases']:
        assert max(0, case['target'] - sum(case['succeeded']) - sum(case['reserved'])) == case['remaining']
    for case in cases['discount_cases']:
        assert (case['sum'] * case['bps'] + 5000) // 10000 == case['discount']
    assert len({c['id'] for c in cases['state_cases']}) == len(cases['state_cases'])

    cards = read(PACKET / 'evidence/cards.json')['data']
    live_by_id = {c['id']: c for c in cards}
    amendments = read(PACKET / 'amendments/manifest.json')
    assert len({a['ticket'] for a in amendments}) == len(amendments) == 25
    for a in amendments:
        assert not a['published']
        old = live_by_id[a['card_id']]['desc']
        assert hashlib.sha256(old.encode()).hexdigest() == a['observed_description_sha256']
        for key in ('amendment', 'proposed_description'):
            content = (PACKET / 'amendments' / a[key + '_file']).read_bytes()
            assert hashlib.sha256(content).hexdigest() == a[key + '_sha256']
        proposed = (PACKET / 'amendments' / a['proposed_description_file']).read_text()
        assert proposed.endswith((PACKET / 'amendments' / a['amendment_file']).read_text())
        assert '## Context' in proposed and 'Acceptance' in proposed
        if a['ticket'] == 'GK-026':
            assert 'start-first rejects with zero refund' not in proposed
            assert 'simultaneous4000p requests accepts only one' not in proposed
        if a['ticket'] == 'GK-023':
            assert 'capacity reduction below held+committed rejects' not in proposed
        if a['ticket'] == 'GK-031':
            assert 'exactly one move succeeds' not in proposed
        if a['ticket'] == 'GK-032':
            assert 'expiry_date:null' not in proposed
        assert len(proposed) <= 16384
    baseline = read(PACKET / 'input-manifest.json')['file_sha256']
    protected = [p for p in baseline if '/sessions/' in p or p == 'docs/design/screens.md']
    assert protected
    for name in protected:
        assert hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == baseline[name], name
    print(f'PASS: design/owner links;40 requirements; dependency+milestone DAG;'
          f'{len(names)} typed relations;17 methods;11 money fixtures;'
          f'{len(cases["state_cases"])} planned state cases;25 exact unpublished amendments;'
          f'{len(protected)} visual inputs unchanged. No application test claim.')


if __name__ == '__main__':
    main()

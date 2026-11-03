#!/usr/bin/env python3
"""Validate role packet mappings against their exact local source revision."""
import hashlib
import json
import re
import runpy
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def main():
    check_document = runpy.run_path(str(ROOT / 'scripts/check_planning_kit.py'))['check_document']
    for path in (ROOT / 'docs/roles').glob('*.md'):
        assert not check_document(path), (path, check_document(path))
    plan = json.loads((ROOT / 'docs/design/ticket-plan.json').read_text())
    sources = {t['id']: t for t in plan}
    published = json.loads((ROOT / 'docs/design/trello-map.json').read_text())
    mapping = json.loads((ROOT / 'docs/roles/mapping.json').read_text())
    rows = mapping['records']
    assert [r['source_key'] for r in rows] == [t['id'] for t in sorted(plan, key=lambda t: t['order'])]
    assert len({r['source_key'] for r in rows}) == len(sources)
    blocks = {}
    for language, prefix in [('developer.en', 'DEV'), ('business-owner.uk', 'BO')]:
        text = (ROOT / f'docs/roles/{language}.md').read_text()
        found = re.findall(rf'<!-- BEGIN ({prefix}-GK-\d{{3}}) -->\n(.*?)<!-- END \1 -->', text, re.S)
        assert len(found) == len(set(k for k, _ in found)), 'duplicate packet key'
        assert len(found) == text.count('<!-- BEGIN '), 'unmatched packet boundary'
        for key, _ in found:
            assert text.count(f'<a id="{key.lower()}"></a>') == 1, (key, 'missing/duplicate anchor')
        blocks[prefix] = dict(found)
    owner_keys = set()
    for row in rows:
        key = row['source_key']
        source = sources[key]
        for field, source_field in [('order', 'order'), ('priority', 'priority'), ('source_wave', 'wave'), ('source_dependencies', 'depends')]:
            assert row[field] == source[source_field], (key, field)
        assert row['source_card_id'] == published[key]['id']
        assert row['source_card_url'] == published[key]['url']
        assert row['live_description_sha256'] == hashlib.sha256(published[key]['linked_desc'].encode()).hexdigest()
        src = ROOT / row['source_path']
        assert row['source_sha256'] == hashlib.sha256(src.read_bytes()).hexdigest(), f'{key}: source changed; refresh role packet'
        dev = row['developer_packet']
        assert dev['path'] == 'docs/roles/developer.en.md'
        assert dev['section_key'] == f'DEV-{key}'
        assert dev.get('packet_format') == 'developer-guided-r3'
        body = blocks['DEV'][dev['section_key']]
        assert body.startswith(f'## DEV-{key} — ')
        assert row['source_card_url'] in body
        for heading in ['Context', 'Before you start', 'Expected outputs', 'To dos', 'Worked output example', 'Verify and hand off', 'Acceptance Criteria']:
            assert f'### {heading}\n' in body, (key, heading)
        for marker in ['**Outcome:**', '**Human handoff:**', '```text\n', 'Illustrative only:']:
            assert marker in body, (key, marker)
        for criterion in re.findall(r'^AC\d+\. .+$', src.read_text(), re.M):
            assert criterion in body, (key, 'source acceptance criterion omitted')
        owner = row['business_owner_packet']
        if owner:
            owner_keys.add(owner['local_key'])
            assert owner['path'] == 'docs/roles/business-owner.uk.md'
            assert owner['local_key'] == owner['section_key'] == f'BO-{key}'
            assert owner['language'] == 'uk' and owner['parent_completion_policy'] == 'partial_contribution_only'
            body = blocks['BO'][owner['section_key']]
            assert body.startswith('## ' + owner['title'] + '\n')
            assert row['source_card_url'] in body and owner['entry_condition'] in body
            for heading in ['Контекст', 'Коли починати', 'Допомога розробника', 'Що зробити', 'Приклад або пояснення', 'Що передати розробнику', 'Коли ваша частина готова', 'Що буде далі']:
                assert f'### {heading}' in body, (key, heading)
            assert owner.get('packet_format') == 'owner-guided-r2'
            assert '```text\n' in body and '**Результат вашої роботи:**' in body
            assert bool(owner['future_card_id']) == bool(owner['future_card_url'])
    assert set(blocks['DEV']) == {f'DEV-{k}' for k in sources}
    assert set(blocks['BO']) == owner_keys
    assert {r['source_key'] for r in rows if not r['business_owner_packet']} == {'GK-013', 'GK-019', 'GK-020', 'GK-034'}
    assert bool(mapping['target_board_id']) == bool(mapping['target_board_url'])
    if mapping['publication_status'] == 'published':
        owners = [r['business_owner_packet'] for r in rows if r['business_owner_packet']]
        assert mapping['target_board_id'] and mapping['publication']['task_count'] == len(owners)
        assert all(b['future_card_id'] and b['future_card_url'] for b in owners)
        assert len({b['future_card_id'] for b in owners}) == len(owners)
        for b in owners:
            for field in ['published_source_body_sha256', 'published_description_sha256']:
                assert re.fullmatch(r'[0-9a-f]{64}', b[field]), (b['local_key'], field)
            source_body = blocks['BO'][b['section_key']].strip().split('\n', 1)[1].strip()
            assert hashlib.sha256(source_body.encode()).hexdigest() == b['published_source_body_sha256'], (b['local_key'], 'owner source changed since publication; reconcile live card')
    assert sources['GK-020']['completion_after'] == ['GK-013']
    assert 'GK-020' in sources['GK-013']['dependency_deliverables']
    print(f'PASS: {len(rows)} source mappings, {len(blocks["DEV"])} developer packets, {len(owner_keys)} Ukrainian owner packets; guided sections/examples/anchors, IDs/order/dependencies/hashes/ACs/links and bootstrap milestones intact.')

if __name__ == '__main__':
    main()

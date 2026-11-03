#!/usr/bin/env python3
"""Check the three owner evidence packets; semantic/translation review stays human."""
import hashlib
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
NUM = re.compile(r'\d+(?:[.:]\d+)*|\b(?:два|двох|двома|обидва|три|один|одного|одній|одне|декілька|кілька|both)\b', re.I)


def digest(text):
    return hashlib.sha256(text.encode()).hexdigest()


def no_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        assert key not in result, f'duplicate JSON key: {key}'
        result[key] = value
    return result


def read(path):
    return json.loads(path.read_text(), object_pairs_hook=no_duplicate_keys,
                      parse_float=lambda _: (_ for _ in ()).throw(AssertionError('float in exact-value asset')))


def main():
    mapping = read(ROOT / 'docs/roles/mapping.json')
    contract = (ROOT / 'docs/design/owner-data-contract.md').read_text()
    all_ids = set()
    numeric_count = 0
    for number in ('001', '003', '005'):
        folder = ROOT / f'docs/creator/GK-{number}'
        source, data = read(folder / 'source-comments.json'), read(folder / 'decisions.json')
        row = next(r for r in mapping['records'] if r['source_key'] == f'GK-{number}')
        snapshots = {s['role']: s for s in source['card_snapshots']}
        assert snapshots['owner']['card']['id'] == row['business_owner_packet']['future_card_id']
        assert snapshots['owner']['card']['idBoard'] == mapping['target_board_id']
        assert snapshots['parent']['card']['id'] == row['source_card_id']
        for snapshot in source['card_snapshots']:
            assert digest(snapshot['card']['desc']) == snapshot['description_sha256']
        assert len({c['id'] for c in source['comments']}) == len(source['comments'])
        comments = {c['id']: c for c in source['comments']}
        decisions = data['decisions']
        history = data['previous_decision_revisions']
        assert len({(d['id'], d['revision_sha256']) for d in history}) == len(history)
        for key, decision in decisions.items():
            did = decision['id']
            assert did == f'BO-GK-{number}.{key}' and did not in all_ids
            all_ids.add(did)
            assert f'/decisions/{key}/business/value' in contract, (did, 'missing SQL map')
            assert f"### {decision['data_contract']} —" in contract, (did, 'unknown data-contract profile')
        for decision in list(decisions.values()) + history:
            body = {k: v for k, v in decision.items() if k != 'revision_sha256'}
            assert digest(json.dumps(body, ensure_ascii=False, sort_keys=True, separators=(',', ':'))) == decision['revision_sha256']
            for provenance in [decision['provenance']] + decision.get('supporting_provenance', []):
                assert (folder / provenance['source_file']).is_file()
                comment = comments[provenance['comment_id']]
                revision = next(r for r in comment['revisions'] if r['sha256_original_utf8'] == provenance['revision_sha256'])
                assert all(1 <= a <= b <= len(revision['text'].splitlines()) for a, b in provenance['lines'])
            assert decision['status'] in data['status_meaning']
            assert {'value', 'type', 'unit', 'scope', 'conditions', 'exceptions', 'unresolved_semantics'} <= decision['business'].keys()
            for related in decision.get('related_policy_refs', []):
                packet = read(folder / related['decision_file'])
                versions = list(packet['decisions'].values()) + packet['previous_decision_revisions']
                assert any(v['id'] == related['decision_id'] and v['revision_sha256'] == related['revision_sha256'] for v in versions)
                evidence = read(folder / related['source_file'])
                cited = next(c for c in evidence['comments'] if c['id'] == related['source_id'])
                assert any(r['sha256_original_utf8'] == related['source_revision_sha256'] for r in cited['revisions'])
        for question in data['questions'].values():
            assert set(question['source_decisions']) <= {d['id'] for d in decisions.values()}
        follow_up_path = folder / 'follow-up.json'
        if follow_up_path.exists():
            follow_up = read(follow_up_path)
            body = (folder / follow_up['body_file']).read_text()
            assert follow_up['ticket'] == data['ticket']
            assert digest(body) == follow_up['body_sha256']
            assert set(follow_up['question_ids']) <= {q['id'] for q in data['questions'].values()}
            assert len(follow_up['question_ids']) == len(set(follow_up['question_ids']))
            publications = follow_up['publication']
            assert len({p['comment_id'] for p in publications}) == len(publications)
            for publication in publications:
                assert publication['card_id'] == row['source_card_id']
                assert digest(publication['text']) == publication['sha256_original_utf8']
                if publication['reviewed_body_sha256'] == follow_up['body_sha256']:
                    # The browser editor reflows blank lines and adds an invisible link title.
                    def content_lines(text):
                        return [line for line in text.replace(' "\u200c")', ')').splitlines() if line]
                    assert content_lines(publication['text']) == content_lines(body)
        for comment in comments.values():
            assert len({r['sha256_original_utf8'] for r in comment['revisions']}) == len(comment['revisions'])
            assert comment['current_revision'] in {r['sha256_original_utf8'] for r in comment['revisions']}
            for revision in comment['revisions']:
                assert digest(revision['text']) == revision['sha256_stored_utf8']
                if not revision['redactions']:
                    assert revision['sha256_original_utf8'] == revision['sha256_stored_utf8']
                lines = revision['text'].splitlines()
                assert [c['line'] for c in revision['coverage']] == list(range(1, len(lines) + 1))
                redacted = {r['line']: r for r in revision['redactions']}
                for coverage in revision['coverage']:
                    line = coverage['line']
                    expected = {d['id'] for d in list(decisions.values()) + history
                                    if any(p['comment_id'] == comment['id']
                                           and p['revision_sha256'] == revision['sha256_original_utf8']
                                           and any(a <= line <= b for a, b in p['lines'])
                                           for p in [d['provenance']] + d.get('supporting_provenance', []))}
                    assert set(coverage['decision_ids']) == expected, (number, line, 'coverage')
                    assert expected or coverage['disposition'] in ('heading', 'formatting', 'submission_metadata', 'quoted_question')
                    occurrences = [x for x in revision['numeric_occurrences'] if x['line'] == line]
                    if line in redacted:
                        assert len(occurrences) == redacted[line]['numeric_identifier_occurrences']
                        assert all(x['written'] is None and x['disposition'] == 'protected_identifier' for x in occurrences)
                    else:
                        assert [x['written'] for x in occurrences] == NUM.findall(lines[line - 1]), (number, line, 'numeric coverage')
                    assert all(set(x['decision_ids']) == expected for x in occurrences)
                    numeric_count += len(occurrences)
        for key, d in decisions.items():
            v = d['business']['value']
            if key in ('refund_example_delivery', 'refund_example'):
                assert v['cake_minor'] * v['cake_percent'] == v['cake_refund_minor'] * 100
                assert v['cake_refund_minor'] + v['delivery_refund_minor'] == v['total_refund_minor']
            elif key == 'refund_example_discount':
                assert v['original_cake_minor'] - v['discount_minor'] == v['paid_cake_minor']
                assert v['paid_cake_minor'] * v['cake_percent'] == v['refund_minor'] * 100
            elif key == 'refund_example_remaining':
                assert v['target_minor'] - v['succeeded_minor'] == v['remaining_minor']
    for path in ('AGENTS.md', 'docs/creator/README.md', 'docs/design/PLAN.md',
                 'docs/roles/README.md', 'skills/gk-plan/references/execution-check.md'):
        assert 'owner-data-contract.md' in (ROOT / path).read_text(), (path, 'missing loading path')
    print(f'PASS: 3 owner packets, {len(all_ids)} decisions, {numeric_count} numeric occurrences; identities, hashes, references, coverage, mapping, exact arithmetic and loading hooks.')


def self_test():
    from contextlib import redirect_stdout
    from io import StringIO
    from unittest.mock import patch

    source_path = ROOT / 'docs/creator/GK-001/source-comments.json'
    source = read(source_path)
    text = '# Metadata-only comment' + '\n' * 8 + '# End'
    revision = {'text': text, 'sha256_original_utf8': digest(text),
                'sha256_stored_utf8': digest(text), 'redactions': [],
                'coverage': [{'line': n, 'disposition': 'heading' if t else 'formatting',
                              'decision_ids': []} for n, t in enumerate(text.splitlines(), 1)],
                'numeric_occurrences': []}
    source['comments'].append({'id': 'synthetic-second-comment',
                              'current_revision': digest(text), 'revisions': [revision]})
    original_read = read
    with patch(__name__ + '.read', side_effect=lambda p: source if p == source_path else original_read(p)):
        with redirect_stdout(StringIO()):
            main()  # Valid extra comment must not inherit another comment's line mapping.
        source['comments'][0]['revisions'].append(dict(revision))
        with redirect_stdout(StringIO()):
            main()  # An independently valid retained revision is also supported.
        source['comments'][0]['revisions'][-1]['sha256_stored_utf8'] = 'invalid'
        try:
            with redirect_stdout(StringIO()):
                main()
        except AssertionError:
            pass
        else:
            raise AssertionError('Corrupted retained history was accepted')
    print('PASS: isolated in-memory multi-comment and retained-history regression checks')


if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        self_test()
    elif sys.argv[1:]:
        sys.exit('Usage: check_owner_intake.py [--self-test]')
    else:
        main()

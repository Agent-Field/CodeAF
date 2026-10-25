"""Paired extraction-cost experiment. Writes private evidence outside the repository."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path

from knowledge import identity, operational_envelope
from provider import MODEL, PROMPT_VERSION, PROMPTS, OpenRouter


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--corpus', type=Path, required=True)
    parser.add_argument('--baseline-calls', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--sample', type=int, default=12)
    args = parser.parse_args()
    if args.sample < 2:
        parser.error('--sample must be at least 2')
    args.out.mkdir(parents=True, exist_ok=True, mode=0o700)
    messages = json.loads(args.corpus.read_text())['messages']
    batches, batch, size, session = [], [], 0, None
    for message in messages:
        if batch and (session != message['session'] or size + len(message['text']) > 24000):
            batches.append(batch)
            batch, size = [], 0
        session = message['session']
        batch.append(message)
        size += len(message['text'])
    if batch:
        batches.append(batch)
    rows = []
    for index, batch in enumerate(batches):
        request_id = identity([MODEL, PROMPT_VERSION, PROMPTS['extract'], {'messages': batch}])
        path = args.baseline_calls / (request_id + '.json')
        if not path.exists():
            raise ValueError(f'Missing baseline batch {index}; use the matching original corpus/cache')
        baseline = json.loads(path.read_text())
        if baseline['model'] != MODEL:
            raise ValueError('Baseline model mismatch')
        rows.append((index, batch, baseline))
    rows.sort(key=lambda row: row[2]['usage']['cost'])
    count = min(args.sample, len(rows))
    if count < 2:
        raise ValueError('Need at least two baseline batches')
    selected = [rows[round((len(rows)-1)*q/(count-1))] for q in range(count)]
    model = OpenRouter(args.out / 'calls', reasoning_effort='none')

    def run(row):
        index, batch, baseline = row
        retained = [m for m in batch if not operational_envelope(m)]
        payload = {'messages': retained}
        candidate = model('extract', payload) if retained else {'claims': []}
        checks = model('compare_extraction', {
            'messages': batch,
            'baseline': [{'id': f'B{i}', **c} for i, c in enumerate(baseline['result']['claims'])],
            'candidate': [{'id': f'N{i}', **c} for i, c in enumerate(candidate['claims'])]})
        cost = 0.0
        usage = {}
        if retained:
            request_id = identity([identity([MODEL, PROMPT_VERSION, PROMPTS['extract'], payload]), {'effort': 'none'}])
            usage = json.loads((model.directory / (request_id + '.json')).read_text())['usage']
            cost = usage.get('cost', 0)
        quote_valid = [c for c in candidate['claims'] if c.get('evidence') and all(
            any(m['id'] == e.get('source') and e.get('quote') and e['quote'] in m['text'] for m in batch)
            for e in c['evidence'])]
        result = {'batch': index, 'baseline_cost': baseline['usage']['cost'], 'candidate_cost': cost,
                  'baseline_usage': baseline['usage'], 'candidate_usage': usage,
                  'baseline_claims': baseline['result']['claims'], 'candidate_claims': candidate['claims'],
                  'quote_valid_candidate_claims': len(quote_valid), 'review': checks}
        (args.out / f'batch-{index}.json').write_text(json.dumps(result, indent=2))
        print(f'paired batch {index}: ${result["baseline_cost"]:.6f} → ${cost:.6f}', flush=True)
        return result

    with ThreadPoolExecutor(max_workers=3) as pool:
        results = list(pool.map(run, selected))
    before = sum(r['baseline_cost'] for r in results)
    after = sum(r['candidate_cost'] for r in results)
    baseline_checks = [dict(batch=r['batch'], **c) for r in results for c in r['review'].get('baseline_checks', [])]
    candidate_checks = [dict(batch=r['batch'], **c) for r in results for c in r['review'].get('candidate_checks', [])]
    summary = {'model': MODEL, 'sample_batches': len(results), 'population_batches': len(rows),
               'population_baseline_cost': sum(r[2]['usage']['cost'] for r in rows),
               'baseline_cost': before, 'candidate_cost': after, 'sample_reduction_percent': 100*(1-after/before) if before else None,
               'baseline_claims': sum(len(r['baseline_claims']) for r in results),
               'candidate_claims': sum(len(r['candidate_claims']) for r in results),
               'quote_valid_candidate_claims': sum(r['quote_valid_candidate_claims'] for r in results),
               'supported_baseline': sum(c.get('supported') is True for c in baseline_checks),
               'supported_baseline_missing': [c for c in baseline_checks if c.get('supported') is True and c.get('covered') is not True],
               'unsupported_candidate': [c for c in candidate_checks if c.get('supported') is not True],
               'review_complete': all(
                   {c.get('id') for c in r['review'].get('baseline_checks', [])} == {f'B{i}' for i in range(len(r['baseline_claims']))} and
                   {c.get('id') for c in r['review'].get('candidate_checks', [])} == {f'N{i}' for i in range(len(r['candidate_claims']))}
                   for r in results),
               'limitation': 'Cost-stratified historical sample; same-model semantic judgments, not human ground truth. Review charges excluded from candidate extraction cost. No whole-pipeline production forecast.'}
    (args.out / 'summary.json').write_text(json.dumps(summary, indent=2))
    print(json.dumps(summary, indent=2))


if __name__ == '__main__':
    main()

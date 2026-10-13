#!/usr/bin/env python3
"""Run a small interoperability smoke exercise against the independent Java participant.

Uses the existing independently authored Java C participant unchanged. No claim
of fresh isolation: this coordinator knows both projects and their published CLIs.
Build Savorum and srefadapter first; pass their paths as the two arguments.
"""
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'sref/conformance'))
import artifact
import semantics

APP, ADAPTER = map(str, map(Path, sys.argv[1:3]))
C = ['java', '-cp', str(ROOT / 'cleanroom/cr-233f3738/participant-c/work/out'), 'sref.cli.Main']
REPORT = {'method': 'existing independent Java participant C; application CLI and conformance adapter', 'cases': [], 'bundle': {}, 'disagreements': []}


def run(command):
    p = subprocess.run(list(map(str, command)), capture_output=True, text=True, timeout=60)
    if p.returncode:
        raise RuntimeError(f'{command[0:2]}: {p.stdout} {p.stderr}')
    return p.stdout


def package(document, path, assets=None):
    assets = assets or {}
    data = json.dumps(document, ensure_ascii=False).encode()
    manifest = {'sref_package': {'version': '0.1.0'}, 'recipe': {'path': 'recipe.json', 'media_type': 'application/json', 'sha256': hashlib.sha256(data).hexdigest(), 'size': len(data)}, 'assets': [{k: a[k] for k in ('id', 'path', 'media_type', 'sha256', 'size')} for a in document.get('assets', [])]}
    with zipfile.ZipFile(path, 'w') as z:
        for name, body in {'recipe.json': data, 'manifest.json': json.dumps(manifest), **assets}.items():
            info = zipfile.ZipInfo(name)
            info.external_attr = 0o100644 << 16
            z.writestr(info, body)


def exchange(source, directory):
    directory.mkdir()
    args = ['--database', directory / 'library.db', '--assets', directory / 'assets']
    run([APP, 'import', *args, '--input', source])
    run([APP, 'export', 'library', *args, '--output', directory / 'export'])
    return list((directory / 'export').glob('*.sref'))


def compare(source, returned):
    problems = artifact.problems(returned.read_bytes(), 'package-writer')
    difference = artifact.divergence(returned.read_bytes(), 'package-writer', source)
    assert not problems, problems
    assert not difference, difference


with tempfile.TemporaryDirectory(prefix='savorum23-') as work:
    work = Path(work)
    fixtures = []
    for kit_path in sorted((ROOT / 'cleanroom/cr-233f3738/kit-staging/examples').glob('*.recipe.json')):
        path = ROOT / 'sref/examples' / kit_path.name
        document = json.loads(path.read_text())
        if document.get('assets'):
            continue  # Bare recipe photographs without bytes are not archive fixtures.
        source = work / (path.stem + '.sref')
        package(document, source)
        fixtures.append(source)
    fixtures += sorted((ROOT / 'cleanroom/cr-233f3738/coordinator/results/unknown-data').glob('*.sref'))
    document = json.loads((ROOT / 'sref/examples/australian-scones.recipe.json').read_text())
    document['id'] = 'interop-image'
    png = base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=')
    document['assets'] = [{'id': 'photo', 'path': 'assets/photo.png', 'media_type': 'image/png', 'sha256': hashlib.sha256(png).hexdigest(), 'size': len(png), 'role': 'image', 'alt': 'Synthetic test photograph'}]
    document['image_refs'] = ['photo']
    document['primary_image'] = 'photo'
    document['instruction_sections'][0]['steps'][0]['image_refs'] = ['photo']
    image_source = work / 'image.sref'
    package(document, image_source, {'assets/photo.png': png})
    fixtures.append(image_source)
    final_packages = []
    for i, source in enumerate(fixtures):
        row = {'fixture': source.name}
        try:
            # Check exact codec preservation independently of application enrichment.
            with zipfile.ZipFile(source) as z:
                document = json.loads(z.read('recipe.json'))
                images = {a['path']: z.read(a['path']) for a in document.get('assets', [])}
            plain = work / f'codec-{i}.json'
            plain.write_text(json.dumps(document))
            answer = json.loads(run([ADAPTER, 'rewrite', plain]))
            assert answer['outcome'] == 'accepted', answer
            codec = work / f'codec-{i}.sref'
            package(json.loads(answer['artifact']), codec, images)
            compare(source, codec)
            c_codec = work / f'c-codec-{i}.sref'
            run([*C, 'repack-package', codec, c_codec])
            compare(source, c_codec)
            with zipfile.ZipFile(c_codec) as z:
                plain.write_bytes(z.read('recipe.json'))
            answer = json.loads(run([ADAPTER, 'rewrite', plain]))
            assert answer['outcome'] == 'accepted', answer
            package(json.loads(answer['artifact']), codec, images)
            compare(source, codec)
            first = exchange(source, work / f'savorum-first-{i}')[0]
            enrichment = artifact.divergence(first.read_bytes(), 'package-writer', source)
            if enrichment:
                row['first_library_import_enrichment'] = enrichment
            produced = work / f'c-{i}.sref'
            run([*C, 'validate-package', first])
            run([*C, 'repack-package', first, produced])
            compare(first, produced)
            final = exchange(produced, work / f'savorum-final-{i}')[0]
            compare(first, final)
            row.update(passed=True, chain='source → Savorum → C → Savorum', semantic=True)
            final_packages.append(final)
        except Exception as exc:
            row.update(passed=False, detail=str(exc))
        REPORT['cases'].append(row)
    if final_packages:
        try:
            bundle = work / 'c.srefbundle'
            run([*C, 'bundle', bundle, *final_packages])
            answer = json.loads(run([ADAPTER, 'check', 'bundle-reader', bundle]))
            assert answer['outcome'] == 'accepted', answer
            returned = exchange(bundle, work / 'bundle-import')
            before = {}
            for path in final_packages:
                with zipfile.ZipFile(path) as z:
                    before[json.loads(z.read('recipe.json'))['id']] = path
            assert len(returned) == len(before), (len(returned), len(before))
            for path in returned:
                with zipfile.ZipFile(path) as z:
                    compare(before[json.loads(z.read('recipe.json'))['id']], path)
            REPORT['bundle'] = {'passed': True, 'members': len(returned), 'chain': 'Savorum packages → C bundle → Savorum import → Savorum packages'}
        except Exception as exc:
            REPORT['bundle'] = {'passed': False, 'detail': str(exc)}
REPORT['disagreements'] = [{'classification': 'intentionally unsupported optional capability', 'detail': 'Frozen C predates authored ingredient choices; its 16-example kit is used. New ingredient-choice example excluded; no frozen participant changed.'}, {'classification': 'documented application enrichment', 'detail': 'First library writes reconcile derived timings from prose for creme-anglaise and roast-chicken. Exact source equivalence is verified separately through the Savorum codec/C/codec chain; subsequent product hops preserve the first library export.'}]
REPORT['savorum_revision'] = run(['git', '-C', ROOT / 'savorum', 'rev-parse', 'HEAD']).strip()
REPORT['sref_revision'] = run(['git', '-C', ROOT / 'sref', 'rev-parse', 'HEAD']).strip()
path = ROOT / 'cleanroom/savorum-23/results.json'
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(json.dumps(REPORT, indent=2) + '\n')
print(json.dumps(REPORT, indent=2))
sys.exit(0 if all(row['passed'] for row in REPORT['cases']) and REPORT['bundle'].get('passed') else 1)

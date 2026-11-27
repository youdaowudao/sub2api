import hashlib
import io
import json
import os
import subprocess
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import Mock, patch

import deploy_image as d

V = '0.2.25-9'
C = 'a' * 40
ID = 'sha256:' + 'b' * 64
M = 'c' * 64
RUN = 12345
LABELS = d.labels_for(V, C, RUN, M)
ROOT = Path(__file__).resolve().parents[2]


def failure(status, message, code='MANIFEST_UNKNOWN'):
    return urllib.error.HTTPError('https://ghcr.io', status, 'test', {},
                                  io.BytesIO(json.dumps({'errors': [{'code': code, 'message': message}]}).encode()))


class RegistryTests(unittest.TestCase):
    def test_probe_accept_and_present_any_kind(self):
        response = Mock(status=200)
        response.__enter__ = Mock(return_value=response)
        response.__exit__ = Mock(return_value=False)
        with patch.object(d, '_registry_token', return_value='fixture'), patch.object(d.urllib.request, 'urlopen', return_value=response) as req:
            self.assertTrue(d.probe_tag(V, 'fixture', 'fixture'))
        accept = req.call_args.args[0].headers['Accept']
        self.assertEqual(set(accept.split(', ')), {
            *d.SINGLE_MANIFEST_TYPES, 'application/vnd.oci.image.index.v1+json',
            'application/vnd.docker.distribution.manifest.list.v2+json'})

    def test_probe_only_explicit_missing(self):
        for message, code in [('manifest unknown', 'MANIFEST_UNKNOWN'), ('name unknown', 'NAME_UNKNOWN')]:
            with self.subTest(message=message), patch.object(d, '_registry_token', return_value='fixture'), \
                    patch.object(d.urllib.request, 'urlopen', side_effect=failure(404, message, code)):
                self.assertFalse(d.probe_tag(V, 'fixture', 'fixture'))
        for status, message, code in [(404, 'OCI manifest found, but accept header does not support OCI manifests', 'MANIFEST_UNKNOWN'),
                                      (404, 'OCI index found, but accept header does not support OCI indexes', 'MANIFEST_UNKNOWN'),
                                      (404, 'unknown shape', 'MANIFEST_UNKNOWN'), (401, 'unauthorized', 'UNAUTHORIZED'),
                                      (403, 'denied', 'DENIED'), (429, 'too many requests', 'TOOMANYREQUESTS')]:
            with self.subTest(status=status, message=message), patch.object(d, '_registry_token', return_value='fixture'), \
                    patch.object(d.urllib.request, 'urlopen', side_effect=failure(status, message, code)):
                with self.assertRaises(RuntimeError):
                    d.probe_tag(V, 'fixture', 'fixture')
        with patch.object(d, '_registry_token', return_value='fixture'), \
                patch.object(d.urllib.request, 'urlopen', side_effect=urllib.error.URLError('offline')):
            with self.assertRaises(RuntimeError):
                d.probe_tag(V, 'fixture', 'fixture')

    def fixture(self):
        manifest = {'schemaVersion': 2, 'mediaType': d.SINGLE_MANIFEST_TYPES[0], 'config': {'digest': ID}, 'layers': []}
        # Raw bytes and configuration digest are distinct, including whitespace.
        raw = json.dumps(manifest, indent=2).encode()
        dg = 'sha256:' + hashlib.sha256(raw).hexdigest()
        obj = {'name': d.PACKAGE+'@'+dg, 'manifest': {'digest': dg, 'mediaType': manifest['mediaType'], 'size': len(raw)},
               'image': {'os': 'linux', 'architecture': 'amd64', 'config': {'Labels': LABELS.copy()}}}
        return raw, dg, obj

    def test_registry_info_metadata_only(self):
        raw, dg, obj = self.fixture()
        with patch.object(d, 'command', side_effect=[raw, json.dumps(obj).encode()]) as command:
            self.assertEqual(d.registry_info(V, LABELS), {'registry_ref': d.PACKAGE+'@'+dg, 'image_id': ID})
        self.assertNotEqual(dg, ID)
        self.assertEqual(command.call_args_list[0].args[0][-2:], ['--raw', d.PACKAGE+':'+V])
        self.assertEqual(command.call_args_list[1].args[0][-3:], ['--format', '{{json .}}', d.PACKAGE+'@'+dg])
        self.assertTrue(all(call.args[0][:4] == ['docker', 'buildx', 'imagetools', 'inspect'] for call in command.call_args_list))

    def test_registry_reject_wrong_descriptor_arch_identity_and_index(self):
        for mutate in (lambda obj: obj['manifest'].update(digest=ID),
                       lambda obj: obj['image'].update(architecture='arm64'),
                       lambda obj: obj['image']['config']['Labels'].update({'org.sub2api.run_id': '1'}),
                       lambda obj: obj['image']['config']['Labels'].update({'org.sub2api.migrations_sha256': '0'*64})):
            raw, _, obj = self.fixture()
            mutate(obj)
            with patch.object(d, 'command', side_effect=[raw, json.dumps(obj).encode()]):
                with self.assertRaises(RuntimeError):
                    d.registry_info(V, LABELS)
        with patch.object(d, 'command', return_value=json.dumps({'schemaVersion': 2, 'mediaType': 'application/vnd.oci.image.index.v1+json'}).encode()) as call:
            with self.assertRaises(RuntimeError):
                d.registry_info(V, LABELS)
            self.assertEqual(call.call_count, 1)

    def test_latest_success_only(self):
        row = {'headBranch': 'main', 'event': 'workflow_dispatch', 'conclusion': 'success',
               'status': 'completed', 'headSha': C, 'databaseId': RUN, 'createdAt': '2026-11-27T00:00:00Z',
               'url': f'{d.SOURCE}/actions/runs/{RUN}'}
        with patch.object(d, 'command', return_value=json.dumps([row]).encode()) as call:
            self.assertEqual(d.latest_success(), row)
        self.assertEqual(call.call_args.args[0][:3], ['gh', 'run', 'list'])
        self.assertIn('success', call.call_args.args[0])
        self.assertIn('workflow_dispatch', call.call_args.args[0])
        for bad in ([], [{**row, 'status': 'in_progress'}], [{**row, 'conclusion': 'failure'}],
                    [{**row, 'headBranch': 'dev'}], [{**row, 'event': 'push'}]):
            with patch.object(d, 'command', return_value=json.dumps(bad).encode()):
                with self.assertRaises(RuntimeError):
                    d.latest_success()


class GitTests(unittest.TestCase):
    def test_real_history_not_numeric_versions_and_migrations(self):
        with tempfile.TemporaryDirectory(prefix='.deploy-git-', dir=Path(__file__).parent) as directory:
            repo = Path(directory)
            def git(*args):
                return subprocess.check_output(['git', '-c', 'user.name=fixture', '-c', 'user.email=fixture@example.invalid',
                                                *args], cwd=repo, stderr=subprocess.DEVNULL).decode().strip()
            git('init', '-b', 'main')
            (repo/'backend/cmd/server').mkdir(parents=True)
            (repo/'backend/migrations').mkdir(parents=True)
            (repo/'backend/migrations/a.sql').write_text('  SELECT 1;\n')
            (repo/'backend/migrations/empty.sql').write_text(' \n')
            def add(version):
                (repo/d.VERSION_FILE).write_text(version+'\n')
                git('add', '.'); git('commit', '-m', version)
                return git('rev-parse', 'HEAD')
            old = add('0.2.25-10')
            newer = add('0.2.25-9')
            self.assertTrue(d.check_progression(repo, '0.2.25-10', old, '0.2.25-9', newer))
            self.assertFalse(d.check_progression(repo, '0.2.25-9', newer, '0.2.25-9', newer))
            with self.assertRaises(RuntimeError):
                d.check_progression(repo, '0.2.25-9', newer, '0.2.25-10', old)
            git('checkout', '-b', 'other', old)
            fork = add('0.2.25-99')
            with self.assertRaises(RuntimeError):
                d.check_progression(repo, '0.2.25-9', newer, '0.2.25-99', fork)
            same = add('0.2.25-9')
            with self.assertRaises(RuntimeError):
                d.check_progression(repo, '0.2.25-9', newer, '0.2.25-9', same)
            with self.assertRaises((RuntimeError, subprocess.CalledProcessError)):
                d.check_progression(repo, '0.2.25-9', newer, '0.2.25-9', '0'*40)
            self.assertEqual(json.loads(d.migrations_bytes(repo, newer)), {'a.sql': hashlib.sha256(b'SELECT 1;').hexdigest()})


class PublishTests(unittest.TestCase):
    def scenario(self, probe, *, smoke_failure=False, registry_failure=False, ref='refs/heads/main', wrong_id=False):
        actions = []
        expected = {'registry_ref': d.PACKAGE+'@sha256:'+'e'*64, 'image_id': 'sha256:'+'f'*64 if wrong_id else ID}
        def execute(args, cwd=None):
            actions.append(tuple(args))
            if smoke_failure and args[0] == 'bash':
                raise RuntimeError('test failed')
        def registry(*args):
            actions.append(('registry',))
            if registry_failure:
                raise RuntimeError('wrong run')
            return expected
        env = {'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_REF': ref}
        labels = d.labels_for(V, C, RUN, hashlib.sha256(b'{}').hexdigest())
        with patch.dict(os.environ, env), patch.object(d, 'version_at', return_value=V), \
                patch.object(d, 'migrations_bytes', return_value=b'{}'), \
                patch.object(d, 'command', side_effect=lambda args, cwd=None: C.encode() if args[0] == 'git' else (b'--platform' if args[-1] == '--help' else b'1.48')), \
                patch.object(d, 'probe_tag', side_effect=probe), patch.object(d, 'registry_info', side_effect=registry), \
                patch.object(d, 'local_image', return_value={'Id': ID, 'Config': {'Labels': labels}}), \
                patch.object(d, 'image_smoke_metadata', side_effect=lambda *args: actions.append(('metadata-smoke',))), \
                patch.object(d, 'run', side_effect=execute):
            try:
                d.publish(ROOT, C, RUN, 'fixture', 'fixture')
                return True, actions
            except RuntimeError:
                return False, actions

    def test_publish_same_image_after_smoke(self):
        ok, actions = self.scenario([False, False])
        self.assertTrue(ok)
        build = next(i for i, a in enumerate(actions) if a[:3] == ('docker', 'buildx', 'build'))
        smoke = next(i for i, a in enumerate(actions) if a[0] == 'bash')
        push = next(i for i, a in enumerate(actions) if a[:3] == ('docker', 'image', 'push'))
        self.assertLess(build, smoke); self.assertLess(smoke, push)
        self.assertIn('--platform', actions[push]); self.assertEqual(actions[push][-1], d.PACKAGE+':'+V)
        self.assertFalse(any('latest' in a or a[:2] == ('git', 'tag') for a in actions))

    def test_no_push_on_failure_race_or_other_run(self):
        for args, kwargs in [([False], {'smoke_failure': True}), ([False, True], {}),
                             ([True], {'registry_failure': True}), ([], {'ref': 'refs/heads/dev'})]:
            with self.subTest(kwargs=kwargs):
                ok, actions = self.scenario(args, **kwargs)
                self.assertFalse(ok)
                self.assertFalse(any(a[:3] == ('docker', 'image', 'push') for a in actions))

    def test_same_original_run_only_repull_and_smoke(self):
        ok, actions = self.scenario([True])
        self.assertTrue(ok)
        self.assertTrue(any(a[:2] == ('docker', 'pull') and '@sha256:' in a[-1] for a in actions))
        self.assertTrue(any(a[0] == 'bash' for a in actions))
        self.assertFalse(any(a[:3] in (('docker', 'image', 'push'), ('docker', 'buildx', 'build')) for a in actions))

    def test_published_identity_mismatch_fails(self):
        ok, actions = self.scenario([False, False], wrong_id=True)
        self.assertFalse(ok)
        self.assertTrue(any(a[:3] == ('docker', 'image', 'push') for a in actions))

    def test_workflow_trigger_and_permissions(self):
        import yaml
        doc = yaml.load((ROOT/'.github/workflows/deploy-image.yml').read_text(), Loader=yaml.BaseLoader)
        self.assertEqual(set(doc['on']), {'workflow_dispatch'})
        self.assertEqual(doc['permissions'], {'contents': 'read'})
        self.assertEqual(doc['jobs']['build-and-publish']['permissions'], {'contents': 'read', 'packages': 'write'})
        self.assertEqual(doc['concurrency']['cancel-in-progress'], 'false')
        steps = doc['jobs']['build-and-publish']['steps']
        self.assertIn('refs/heads/main', steps[0]['run'])
        self.assertEqual(steps[1]['with']['ref'], '${{ github.sha }}')


class SmokeShellTests(unittest.TestCase):
    def test_isolated_smoke_and_label_cleanup_on_success_and_failure(self):
        for failure in ('', 'app-health', 'app-create'):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory(prefix='.smoke-fixture-', dir=Path(__file__).parent) as directory:
                root = Path(directory); log = root/'calls'; state = root/'state.json'; docker = root/'docker'
                docker.write_text(r'''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
a = sys.argv[1:]; log=Path(os.environ['FIXTURE_LOG']); state=Path(os.environ['FIXTURE_STATE'])
s = json.loads(state.read_text()) if state.exists() else {'containers': [], 'network': False}
with log.open('a') as f:
    f.write(json.dumps([x.split('=', 1)[0]+'=<fixture>' if 'PASSWORD=' in x else x for x in a])+'\n')
def save(): state.write_text(json.dumps(s))
if a[:2] == ['network', 'create']:
    s['network'] = True; save(); print('network-id')
elif a[0] == 'run':
    name=a[a.index('--name')+1]; s['containers'].append(name); save(); print(name)
    if name.endswith('-app') and os.environ['FIXTURE_FAILURE']=='app-create': sys.exit(1)
elif a[0] == 'exec':
    if 'redis-cli' in a: print('PONG')
    if 'wget' in a: print(json.dumps({'code': 0, 'data': {'version': os.environ['FIXTURE_VERSION']}}))
elif a[0] == 'inspect':
    print('running unhealthy' if os.environ['FIXTURE_FAILURE']=='app-health' else 'running healthy')
elif a[:2] == ['container', 'ls']:
    print('\n'.join(s['containers']))
elif a[:2] == ['container', 'rm']:
    s['containers'].remove(a[-1]); save()
elif a[:2] == ['network', 'ls']:
    if s['network']: print('network-id')
elif a[:2] == ['network', 'rm']:
    s['network']=False; save()
elif a[0] != 'pull': sys.exit(2)
''')
                docker.chmod(0o700)
                env = {**os.environ, 'PATH': str(root)+os.pathsep+os.environ['PATH'], 'FIXTURE_LOG': str(log),
                       'FIXTURE_STATE': str(state), 'FIXTURE_FAILURE': failure, 'FIXTURE_VERSION': V}
                result = subprocess.run(['bash', str(ROOT/'.github/release-tools/deploy_image_smoke.sh'), 'fixture-image', V],
                                        env=env, capture_output=True, text=True, timeout=15)
                self.assertEqual(result.returncode == 0, not failure, result.stderr)
                self.assertEqual(json.loads(state.read_text()), {'containers': [], 'network': False})
                calls = [json.loads(line) for line in log.read_text().splitlines()]
                self.assertTrue(any(a[:2] == ['network', 'create'] and '--internal' in a for a in calls))
                self.assertTrue(all('-p' not in a and '--publish' not in a and '--label' in a for a in calls if a[0] == 'run'))
                self.assertTrue(any(a[:2] == ['container', 'ls'] and any(x.startswith('label=org.sub2api.deploy-smoke=') for x in a) for a in calls))


if __name__ == '__main__':
    unittest.main()

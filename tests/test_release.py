import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError

spec = importlib.util.spec_from_file_location('release', Path(__file__).resolve().parents[1] / 'scripts/release.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
SHA = 'a' * 40
DIGEST = 'sha256:' + 'b' * 64
DIGESTS = {name: DIGEST for name in release.IMAGES}


def document(value):
    raw = json.dumps(value).encode()
    return value, raw, {'Docker-Content-Digest': 'sha256:' + hashlib.sha256(raw).hexdigest()}


class ReleaseTests(unittest.TestCase):
    def test_registry_redirect_drops_cross_host_credentials(self):
        handler = release.RegistryRedirect()
        request = release.urllib.request.Request('https://ghcr.io/blob', headers={'Authorization': 'Bearer test'})
        redirected = handler.redirect_request(request, None, 302, 'Found', {}, 'https://cdn.example/blob')
        self.assertIsNone(redirected.get_header('Authorization'))
        with self.assertRaises(ValueError):
            handler.redirect_request(request, None, 302, 'Found', {}, 'http://cdn.example/blob')

    def test_payload_contract(self):
        result = release.payload('v1.2.3', SHA, DIGESTS)
        self.assertEqual(result['ref'], 'main')
        self.assertEqual(set(result['inputs']), {'app', 'tag', 'sha', 'images'})
        self.assertEqual(json.loads(result['inputs']['images']), {name: f'ghcr.io/michael-duren/{name}@{DIGEST}' for name in release.IMAGES})
        for tag in ['main', 'v1.2-rc1', 'v1+build', 'v1.2.3.4', 'v1\n']:
            with self.assertRaises(ValueError):
                release.payload(tag, SHA, DIGESTS)
        with self.assertRaises(ValueError):
            release.payload('v1', SHA, {**DIGESTS, 'rubber-duck': 'latest'})

    def test_incomplete_promotion_is_rejected(self):
        for name in release.IMAGES:
            with self.assertRaises(ValueError):
                release.payload('v1', SHA, {key: val for key, val in DIGESTS.items() if key != name})
        with self.assertRaises(ValueError):
            release.payload('v1', SHA, {**DIGESTS, 'extra': DIGEST})

    def test_real_git_event_gate(self):
        with tempfile.TemporaryDirectory() as directory:
            old = os.getcwd()
            os.chdir(directory)
            try:
                def git(*args):
                    return subprocess.check_output(['git', *args], stderr=subprocess.DEVNULL, text=True).strip()
                git('init', '-b', 'main')
                git('config', 'commit.gpgsign', 'false')
                git('config', 'tag.gpgsign', 'false')
                git('config', 'user.name', 'Test')
                git('config', 'user.email', 'test@example.invalid')
                git('commit', '--allow-empty', '-m', 'first')
                first = git('rev-parse', 'HEAD')
                git('tag', '-a', 'v1', '-m', 'annotated')
                git('update-ref', 'refs/remotes/origin/main', first)
                push = {'ref': 'refs/tags/v1', 'deleted': False}
                self.assertEqual(release.gate(push, 'push', first), ('v1', first))
                self.assertEqual(release.gate(push, 'push', git('rev-parse', 'v1')), ('v1', first))
                event = {'action': 'published', 'release': {'tag_name': 'v1', 'prerelease': False, 'draft': False}}
                self.assertEqual(release.gate(event, 'release', first), ('v1', first))
                event['release']['prerelease'] = True
                with self.assertRaises(ValueError):
                    release.gate(event, 'release', first)
                with self.assertRaises(ValueError):
                    release.gate({**push, 'deleted': True}, 'push', first)
                git('checkout', '--orphan', 'unmerged')
                git('commit', '--allow-empty', '-m', 'unmerged')
                other = git('rev-parse', 'HEAD')
                git('tag', '-f', 'v1')
                with self.assertRaises(ValueError):
                    release.gate(push, 'push', first)
                with self.assertRaises(subprocess.CalledProcessError):
                    release.gate(push, 'push', other)
            finally:
                os.chdir(old)

    @patch.dict(os.environ, {'GHCR_USER': 'test', 'GHCR_TOKEN': 'test'})
    def test_registry_reuse_checks_content_and_revision(self):
        config = document({'os': 'linux', 'architecture': 'amd64', 'config': {'Labels': {
            'org.opencontainers.image.revision': SHA, 'org.opencontainers.image.source': release.SOURCE}}})
        manifest = document({'config': {'digest': config[2]['Docker-Content-Digest']}})
        index = document({'manifests': [{'platform': {'os': 'linux', 'architecture': 'amd64'},
                                         'digest': manifest[2]['Docker-Content-Digest']}]})
        responses = [document({'token': 'test'}), index, manifest, config]
        with patch.object(release, 'request_json', side_effect=responses):
            self.assertEqual(release.canonical(SHA, 'rubber-duck-runner-go'), index[2]['Docker-Content-Digest'])
        bad_config = document({'os': 'linux', 'architecture': 'amd64', 'config': {'Labels': {
            'org.opencontainers.image.revision': 'c' * 40, 'org.opencontainers.image.source': release.SOURCE}}})
        bad_manifest = document({'config': {'digest': bad_config[2]['Docker-Content-Digest']}})
        with patch.object(release, 'request_json', side_effect=[responses[0], bad_manifest, bad_config]):
            with self.assertRaisesRegex(ValueError, 'revision'):
                release.canonical(SHA, 'rubber-duck-runner-go')
        tampered = (manifest[0], manifest[1], {'Docker-Content-Digest': DIGEST})
        with patch.object(release, 'request_json', side_effect=[responses[0], tampered]):
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                release.canonical(SHA, 'rubber-duck-runner-go')

    @patch.dict(os.environ, {'GHCR_USER': 'test', 'GHCR_TOKEN': 'test'})
    def test_only_explicit_manifest_absence_allows_publish(self):
        for code, error_code, missing in [(404, 'MANIFEST_UNKNOWN', True), (404, 'NAME_UNKNOWN', True), (404, 'UNKNOWN', False),
                                          (403, 'DENIED', False), (401, 'UNAUTHORIZED', False),
                                          (429, 'TOOMANYREQUESTS', False), (500, 'UNKNOWN', False)]:
            error = HTTPError('https://ghcr.io', code, 'registry error', {},
                              io.BytesIO(json.dumps({'errors': [{'code': error_code}]}).encode()))
            with patch.object(release, 'request_json', side_effect=[document({'token': 'test'}), error]):
                if missing:
                    self.assertIsNone(release.canonical(SHA, 'rubber-duck-runner-go'))
                else:
                    with self.assertRaises(HTTPError):
                        release.canonical(SHA, 'rubber-duck-runner-go')
            error.close()
        with patch.object(release, 'request_json', side_effect=URLError('network unavailable')):
            with self.assertRaises(URLError):
                release.canonical(SHA, 'rubber-duck-runner-go')


if __name__ == '__main__':
    unittest.main()

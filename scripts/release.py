#!/usr/bin/env python3
"""Fail-closed release gate, GHCR canonical lookup and promotion payload."""
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import urllib.error
import urllib.request
from urllib.parse import urlsplit

REPO = 'michael-duren/rubber-duck'
IMAGES = {'rubber-duck', 'rubber-duck-runner-go', 'rubber-duck-runner-python', 'rubber-duck-runner-c'}
SOURCE = f'https://github.com/{REPO}'
ACCEPT = ', '.join(['application/vnd.oci.image.index.v1+json',
                    'application/vnd.docker.distribution.manifest.list.v2+json',
                    'application/vnd.oci.image.manifest.v1+json',
                    'application/vnd.docker.distribution.manifest.v2+json'])


def validate(tag, sha):
    if not re.fullmatch(r'v[0-9]+(?:\.[0-9]+){0,2}', tag):
        raise ValueError('Expected stable numeric v1, v1.2 or v1.2.3 tag')
    if not re.fullmatch(r'[0-9a-f]{40}', sha):
        raise ValueError('Expected full lowercase source SHA')


def gate(event, name, event_sha):
    if name == 'push':
        if event.get('deleted') or not event.get('ref', '').startswith('refs/tags/'):
            raise ValueError('Expected non-deleted tag push')
        tag = event['ref'][10:]
    elif name == 'release':
        release = event['release']
        if event.get('action') != 'published' or release.get('prerelease') or release.get('draft'):
            raise ValueError('Expected published stable release')
        tag = release['tag_name']
    else:
        raise ValueError('Unsupported release event')
    validate(tag, event_sha)
    sha = subprocess.check_output(['git', 'rev-parse', f'refs/tags/{tag}^{{commit}}'], text=True).strip()
    event_commit = subprocess.check_output(['git', 'rev-parse', f'{event_sha}^{{commit}}'], text=True).strip()
    if sha != event_commit:
        raise ValueError('Tag moved or event SHA does not match tagged commit')
    subprocess.run(['git', 'merge-base', '--is-ancestor', sha, 'refs/remotes/origin/main'], check=True)
    return tag, sha


class RegistryRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if urlsplit(newurl).scheme != 'https':
            raise ValueError('Registry redirect must use HTTPS')
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        if redirected is not None and urlsplit(req.full_url).netloc != urlsplit(newurl).netloc:
            redirected.remove_header('Authorization')
        return redirected


def request_json(url, headers):
    opener = urllib.request.build_opener(RegistryRedirect())
    with opener.open(urllib.request.Request(url, headers=headers), timeout=60) as response:
        raw = response.read()
        return json.loads(raw), raw, response.headers


def canonical(sha, image):
    if image not in IMAGES:
        raise ValueError("Unknown image")
    image_repo = f"michael-duren/{image}"
    validate('v1', sha)
    basic = base64.b64encode(f"{os.environ['GHCR_USER']}:{os.environ['GHCR_TOKEN']}".encode()).decode()
    token, _, _ = request_json(f'https://ghcr.io/token?service=ghcr.io&scope=repository:{image_repo}:pull',
                               {'Authorization': f'Basic {basic}'})
    headers = {'Authorization': f"Bearer {token['token']}", 'Accept': ACCEPT}
    base = f'https://ghcr.io/v2/{image_repo}'
    try:
        manifest, raw, response_headers = request_json(f'{base}/manifests/sha-{sha}', headers)
    except urllib.error.HTTPError as error:
        # Only explicit registry manifest/repository absence permits building. Auth, transport,
        # rate limits and generic 404s fail rather than authorizing overwrite.
        if error.code == 404:
            body = json.loads(error.read())
            errors = body.get('errors', [])
            if errors and all(item.get('code') in {'MANIFEST_UNKNOWN', 'NAME_UNKNOWN'} for item in errors):
                return None
        raise
    digest = 'sha256:' + hashlib.sha256(raw).hexdigest()
    if response_headers.get('Docker-Content-Digest') != digest:
        raise ValueError('Canonical manifest digest mismatch')
    if 'manifests' in manifest:
        matches = [item for item in manifest['manifests']
                   if item.get('platform', {}).get('os') == 'linux'
                   and item.get('platform', {}).get('architecture') == 'amd64']
        if len(matches) != 1:
            raise ValueError('Canonical image must contain one linux/amd64 manifest')
        descriptor = matches[0]['digest']
        manifest, raw, _ = request_json(f'{base}/manifests/{descriptor}', headers)
        if descriptor != 'sha256:' + hashlib.sha256(raw).hexdigest():
            raise ValueError('Platform manifest digest mismatch')
    config_digest = manifest['config']['digest']
    config, raw, _ = request_json(f'{base}/blobs/{config_digest}', headers)
    if config_digest != 'sha256:' + hashlib.sha256(raw).hexdigest():
        raise ValueError('Image config digest mismatch')
    labels = config.get('config', {}).get('Labels', {})
    if labels.get('org.opencontainers.image.revision') != sha or labels.get('org.opencontainers.image.source') != SOURCE:
        raise ValueError('Canonical image source/revision mismatch; refusing reuse or overwrite')
    if config.get('os') != 'linux' or config.get('architecture') != 'amd64':
        raise ValueError('Canonical image must target linux/amd64')
    return digest


def payload(tag, sha, digests):
    validate(tag, sha)
    if set(digests) != IMAGES:
        raise ValueError('All four app/runner digests are required atomically')
    if any(not re.fullmatch(r'sha256:[0-9a-f]{64}', digest) for digest in digests.values()):
        raise ValueError('Expected immutable SHA256 image digests')
    return {'ref': 'main', 'inputs': {'app': 'rubber-duck', 'tag': tag, 'sha': sha,
            'images': json.dumps({name: f'ghcr.io/michael-duren/{name}@{digest}'
                                  for name, digest in digests.items()}, separators=(',', ':'))}}


if __name__ == '__main__':
    command = sys.argv[1]
    if command == 'gate':
        tag, sha = gate(json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text()),
                        os.environ['GITHUB_EVENT_NAME'], os.environ['GITHUB_SHA'])
        with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
            output.write(f'tag={tag}\nsha={sha}\n')
    elif command == 'lookup':
        digest = canonical(sys.argv[2], sys.argv[3])
        with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
            output.write(f'exists={str(digest is not None).lower()}\ndigest={digest or ""}\n')
    elif command == 'payload':
        print(json.dumps(payload(sys.argv[2], sys.argv[3], json.loads(Path(sys.argv[4]).read_text()))))
    else:
        raise ValueError('Unknown command')

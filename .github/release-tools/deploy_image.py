"""Manual deployment image helpers. No production host or credentials live here."""
import base64
import hashlib
import io
import json
import os
import re
import subprocess
import sys
import tarfile
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

OWNER_REPO = 'youdaowudao/sub2api'
PACKAGE = 'ghcr.io/youdaowudao/sub2api-deploy'
PACKAGE_PATH = PACKAGE.removeprefix('ghcr.io/')
SOURCE = 'https://github.com/' + OWNER_REPO
VERSION_FILE = 'backend/cmd/server/VERSION'
VERSION_RE = re.compile(r'[0-9]+\.[0-9]+\.[0-9]+-[1-9][0-9]*')
COMMIT_RE = re.compile(r'[0-9a-f]{40}')
DIGEST_RE = re.compile(r'sha256:[0-9a-f]{64}')
HASH_RE = re.compile(r'[0-9a-f]{64}')
SINGLE_MANIFEST_TYPES = ('application/vnd.oci.image.manifest.v1+json',
                         'application/vnd.docker.distribution.manifest.v2+json')
MANIFEST_ACCEPT = ', '.join((*SINGLE_MANIFEST_TYPES,
    'application/vnd.oci.image.index.v1+json',
    'application/vnd.docker.distribution.manifest.list.v2+json'))
RUN_FIELDS = 'databaseId,headSha,headBranch,event,status,conclusion,createdAt,number,url'


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def command(args, cwd=None):
    result = subprocess.run(args, cwd=cwd, check=True, stdout=subprocess.PIPE)
    return result.stdout


def run(args, cwd=None):
    subprocess.run(args, cwd=cwd, check=True)


def canonical(value):
    return (json.dumps(value, sort_keys=True) + '\n').encode()


def validate_version(version):
    require(isinstance(version, str) and VERSION_RE.fullmatch(version), 'VERSION 格式不符')
    return version


def validate_commit(commit):
    require(isinstance(commit, str) and COMMIT_RE.fullmatch(commit), 'Git 提交格式不符')
    return commit


def validate_digest(digest):
    require(isinstance(digest, str) and DIGEST_RE.fullmatch(digest), '镜像摘要格式不符')
    return digest


def version_at(repo, commit):
    validate_commit(commit)
    return validate_version(command(['git', 'show', f'{commit}:{VERSION_FILE}'], repo).decode().strip())


def is_ancestor(repo, parent, child):
    # Both parameters are fixed full commit IDs, not arbitrary Git options.
    validate_commit(parent)
    validate_commit(child)
    rc = subprocess.run(['git', 'merge-base', '--is-ancestor', parent, child], cwd=repo).returncode
    require(rc in (0, 1), '无法确认 Git 提交祖先关系，停止')
    return rc == 0


def check_progression(repo, current_version, current_commit, version, commit):
    require(version_at(repo, current_commit) == current_version, '当前部署的 Git 提交与版本不符')
    require(version_at(repo, commit) == version, '目标 Git 提交与版本不符')
    if version == current_version or commit == current_commit:
        require(version == current_version and commit == current_commit, '同版本或同提交身份冲突')
        return False  # caller must still compare the actual image identities
    require(is_ancestor(repo, current_commit, commit), '目标不是当前部署提交的严格后继，停止；历史回退请用 rollback')
    return True


def migrations_bytes(repo, commit):
    validate_commit(commit)
    data = command(['git', 'archive', '--format=tar', commit, 'backend/migrations'], repo)
    result = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:') as archive:
        for item in archive:
            path = Path(item.name)
            if not item.isfile() or path.parent != Path('backend/migrations') or path.suffix != '.sql':
                continue
            raw = archive.extractfile(item).read().decode('utf-8').strip().encode()
            if raw:
                result[path.name] = hashlib.sha256(raw).hexdigest()
    require(result, '没有迁移文件')
    return canonical(result)


def labels_for(version, commit, run_id, migration_hash):
    validate_version(version)
    validate_commit(commit)
    require(re.fullmatch(r'[1-9][0-9]*', str(run_id)), '运行 ID 不符')
    require(HASH_RE.fullmatch(migration_hash), '迁移清单指纹不符')
    return {'org.opencontainers.image.source': SOURCE, 'org.sub2api.version': version,
            'org.sub2api.commit': commit, 'org.sub2api.run_id': str(run_id),
            'org.sub2api.migrations_sha256': migration_hash}


def latest_success(repo=OWNER_REPO):
    rows = json.loads(command(['gh', 'run', 'list', '--repo', repo, '--workflow', 'deploy-image.yml',
                               '--branch', 'main', '--event', 'workflow_dispatch', '--status', 'success',
                               '--limit', '1', '--json', RUN_FIELDS]))
    require(isinstance(rows, list) and len(rows) == 1, '没有成功的手动 main 镜像构建')
    row = rows[0]
    require(row.get('headBranch') == 'main' and row.get('event') == 'workflow_dispatch'
            and row.get('status') == 'completed' and row.get('conclusion') == 'success', '构建运行身份不符')
    validate_commit(row.get('headSha'))
    require(type(row.get('databaseId')) is int and row['databaseId'] > 0, '构建运行 ID 不符')
    require(row.get('url') == f'{SOURCE}/actions/runs/{row["databaseId"]}', '构建链接不符')
    require(isinstance(row.get('createdAt'), str) and row['createdAt'], '缺少运行时间')
    return row


def registry_info(version, expected_labels):
    """Metadata only: raw single manifest, then descriptor/config at its digest."""
    validate_version(version)
    raw = command(['docker', 'buildx', 'imagetools', 'inspect', '--raw', f'{PACKAGE}:{version}'])
    manifest = json.loads(raw)
    require(manifest.get('mediaType') in SINGLE_MANIFEST_TYPES and manifest.get('schemaVersion') == 2,
            'GHCR 不是单平台镜像 manifest')
    config_id = validate_digest(manifest.get('config', {}).get('digest'))
    digest = 'sha256:' + hashlib.sha256(raw).hexdigest()
    ref = f'{PACKAGE}@{digest}'
    obj = json.loads(command(['docker', 'buildx', 'imagetools', 'inspect', '--format', '{{json .}}', ref]))
    descriptor, image = obj.get('manifest', {}), obj.get('image', {})
    require(descriptor.get('digest') == digest and descriptor.get('mediaType') == manifest['mediaType'],
            'GHCR manifest 摘要或类型不符')
    require(image.get('os') == 'linux' and image.get('architecture') == 'amd64', '镜像架构不符')
    labels = image.get('config', {}).get('Labels') or {}
    require(all(labels.get(k) == v for k, v in expected_labels.items()), '镜像版本/提交/运行 ID/迁移清单标签不符')
    return {'registry_ref': ref, 'config_digest': config_id}


def _registry_token(actor, token):
    url = 'https://ghcr.io/token?' + urllib.parse.urlencode({
        'service': 'ghcr.io', 'scope': f'repository:{PACKAGE_PATH}:pull,push'})
    auth = base64.b64encode(f'{actor}:{token}'.encode()).decode()
    req = urllib.request.Request(url, headers={'Authorization': 'Basic ' + auth})
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            data = json.load(response)
    except (urllib.error.URLError, ValueError):
        raise RuntimeError('GHCR 认证失败，停止发布') from None
    bearer = data.get('token') or data.get('access_token')
    require(isinstance(bearer, str) and bearer, 'GHCR 未返回有效认证令牌')
    return bearer


def probe_tag(version, actor, token):
    """True for any successful GET, False only for explicit absent errors.

    Unknown responses fail closed, including OCI content negotiation failures.
    The real first publication must verify these GHCR error shapes.
    """
    validate_version(version)
    require(actor and token, '缺少 GHCR 工作流认证')
    bearer = _registry_token(actor, token)
    req = urllib.request.Request(f'https://ghcr.io/v2/{PACKAGE_PATH}/manifests/{version}',
                                 headers={'Authorization': 'Bearer ' + bearer, 'Accept': MANIFEST_ACCEPT})
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            require(200 <= response.status < 300, 'GHCR 标签查询返回异常')
            return True
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            try:
                items = json.load(exc)['errors']
                if isinstance(items, list) and len(items) == 1:
                    error = items[0]
                    # Exact known absence messages, never a content-negotiation error.
                    if (error.get('code'), error.get('message', '').lower()) in {
                        ('MANIFEST_UNKNOWN', 'manifest unknown'),
                        ('NAME_UNKNOWN', 'name unknown'),
                        ('NAME_UNKNOWN', 'repository name not known to registry'),
                    }:
                        return False
            except (ValueError, TypeError, KeyError, AttributeError):
                pass
        raise RuntimeError(f'GHCR 标签查询未能确认存在性（HTTP {exc.code}），停止发布') from None
    except urllib.error.URLError:
        raise RuntimeError('GHCR 标签查询网络失败，停止发布') from None


def parse_version_output(output, version, commit):
    require(re.search(r'Sub2API ' + re.escape(version) + r' \(commit: ' + re.escape(commit) + r',', output),
            '镜像内程序版本或提交不符')


def parse_binary_hash(output):
    parts = output.split()
    require(parts and HASH_RE.fullmatch(parts[0]), '程序指纹格式错误')
    return parts[0]


def local_image(image):
    obj = json.loads(command(['docker', 'image', 'inspect', image]))[0]
    require(obj.get('Os') == 'linux' and obj.get('Architecture') == 'amd64', '镜像架构不符')
    validate_digest(obj.get('Id'))
    return obj


def registry_digests(receipt):
    ref = receipt['registry_ref']
    require(isinstance(ref, str) and ref.startswith(PACKAGE+'@'), '固定 GHCR 镜像引用不符')
    return validate_digest(ref.split('@', 1)[1]), validate_digest(receipt['config_digest'])


def matches_registry_id(local_id, receipt):
    # Classic Docker uses the config ID; containerd uses the target manifest ID.
    # Both are exact digests from the same verified single-platform registry manifest.
    return local_id in registry_digests(receipt)


def verify_local_image_identity(image, receipt, *, require_repo_digest=False):
    manifest_digest, config_digest = registry_digests(receipt)
    local_id = validate_digest(image.get('Id'))
    require(image.get('Os') == 'linux' and image.get('Architecture') == 'amd64', '镜像架构不符')
    require(local_id in (manifest_digest, config_digest),
            f'镜像 ID {local_id} 不等于已核对的 manifest {manifest_digest} 或 config {config_digest}')
    descriptor = image.get('Descriptor')
    if descriptor:
        require(isinstance(descriptor, dict) and descriptor.get('mediaType') in SINGLE_MANIFEST_TYPES and
                descriptor.get('digest') == manifest_digest and local_id == manifest_digest,
                '本地镜像描述符与固定单平台 GHCR manifest 不符')
    if require_repo_digest:
        require(receipt['registry_ref'] in (image.get('RepoDigests') or []), '本地镜像缺少已锁定的 GHCR RepoDigest')
    return local_id


def image_smoke_metadata(image, version, commit):
    local_image(image)
    output = command(['docker', 'run', '--rm', '--network', 'none', '--read-only', '--entrypoint',
                      '/app/sub2api', image, '-version']).decode()
    parse_version_output(output, version, commit)
    binary_hash = parse_binary_hash(command(['docker', 'run', '--rm', '--network', 'none', '--read-only',
                                           '--entrypoint', 'sha256sum', image, '/app/sub2api']).decode())
    require(command(['docker', 'run', '--rm', '--network', 'none', '--entrypoint', 'stat', image,
                     '-c', '%a', '/app/docker-entrypoint.sh']).decode().strip() == '755', '镜像启动脚本权限不是755')
    print(f'PASS 镜像版本、提交、架构、入口权限、程序指纹 {binary_hash}')


def publish(repo, commit, run_id, actor, token):
    """Only workflow_dispatch main; build once, test exactly that content, then push."""
    require(os.environ.get('GITHUB_EVENT_NAME') == 'workflow_dispatch' and os.environ.get('GITHUB_REF') == 'refs/heads/main',
            '只能从 main 手动运行')
    validate_commit(commit)
    require(command(['git', 'rev-parse', 'HEAD'], repo).decode().strip() == commit, 'checkout 提交不符')
    version = version_at(repo, commit)
    migration_hash = hashlib.sha256(migrations_bytes(repo, commit)).hexdigest()
    labels = labels_for(version, commit, run_id, migration_hash)
    target = f'{PACKAGE}:{version}'
    existing = probe_tag(version, actor, token)
    if existing:
        # Matching run_id only; never push or overwrite a previously published tag.
        receipt = registry_info(version, labels)
        image = receipt['registry_ref']
        run(['docker', 'pull', '--platform', 'linux/amd64', image])
        verify_local_image_identity(local_image(image), receipt, require_repo_digest=True)
    else:
        api = command(['docker', 'version', '--format', '{{.Server.APIVersion}}']).decode().strip()
        require(re.fullmatch(r'[0-9]+\.[0-9]+', api) and tuple(map(int, api.split('.'))) >= (1, 46),
                'Docker API 不支持单平台 push')
        require(b'--platform' in command(['docker', 'image', 'push', '--help']), 'Docker CLI 不支持单平台 push')
        image = f'sub2api-deploy-build:{version}'
        args = ['docker', 'buildx', 'build', '--platform', 'linux/amd64', '--load', '--tag', image,
                '--build-arg', f'VERSION={version}', '--build-arg', f'COMMIT={commit}',
                '--build-arg', 'DATE=' + datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')]
        for key, value in labels.items():
            args += ['--label', f'{key}={value}']
        run([*args, '.'], repo)
        receipt = None
    tested = local_image(image)
    require(all((tested.get('Config', {}).get('Labels') or {}).get(k) == v for k, v in labels.items()),
            '待试运行镜像标签与构建身份不符')
    tested_id = tested['Id']
    image_smoke_metadata(image, version, commit)
    run(['bash', str(Path(repo) / '.github/release-tools/deploy_image_smoke.sh'), image, version])
    require(local_image(image)['Id'] == tested_id, '试运行期间镜像 ID 改变')
    if not existing:
        require(not probe_tag(version, actor, token), '构建期间同版本已发布，停止，不覆盖')
        run(['docker', 'tag', image, target])
        require(local_image(target)['Id'] == tested_id, '发布标签不是已试运行镜像')
        run(['docker', 'image', 'push', '--platform', 'linux/amd64', target])
        receipt = registry_info(version, labels)
    verify_local_image_identity(tested, receipt, require_repo_digest=existing)
    print(f'PASS 核对本地镜像 ID {tested_id}、GHCR manifest {registry_digests(receipt)[0]}、config {receipt["config_digest"]}')
    print(f'PASS {version} {commit} {receipt["registry_ref"]} {SOURCE}/actions/runs/{run_id}')


def main():
    require(sys.argv[1:] == ['publish'], '用法：deploy_image.py publish')
    publish(Path.cwd(), os.environ['GITHUB_SHA'], os.environ['GITHUB_RUN_ID'],
            os.environ['GITHUB_ACTOR'], os.environ['GITHUB_TOKEN'])


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, ValueError, KeyError, subprocess.CalledProcessError, OSError) as exc:
        # subprocess command lines never contain the registry token.
        print('停止：' + str(exc), file=sys.stderr)
        sys.exit(1)

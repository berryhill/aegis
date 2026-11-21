"""Build private review artifacts. Run as the deployment packager, never as root."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tarfile
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
BASE = '5a52faf1aed965b8692ec890a7d8af6e37b5e59e'
FILES = ('cli.py', 'hermes_cli/cli_stream_mixin.py', 'tui_gateway/agent_factory.py')


def run(*args, cwd=ROOT, **kw):
    return subprocess.check_output(args, cwd=cwd, **kw)


def target(path):
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)) as file:
        info = os.fstat(file.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077 or info.st_nlink != 1:
            raise ValueError('expected private owner-only target file')
        data = json.loads(file.read(4097))
    patterns = {'config': r'/(?:[\w.-]+/)+aegis\.yaml', 'socket': r'/(?:[\w.-]+/)+aegis\.sock',
                'origin': r'https://[a-z0-9.-]+:8448', 'agent': r'[a-z][a-z0-9_]{0,31}',
                'provider': r'[a-z0-9][a-z0-9.-]{0,63}', 'model': r'[a-zA-Z0-9][a-zA-Z0-9.-]{0,95}'}
    if data.keys() != patterns.keys() or not all(isinstance(data[k], str) and re.fullmatch(p, data[k], re.ASCII) for k, p in patterns.items()):
        raise ValueError('unsupported target fields')
    for k in ('config', 'socket'):
        if '.' in data[k].split('/') or '..' in data[k].split('/'):
            raise ValueError('noncanonical target path')
    return data


def build(args):
    os.umask(0o077)
    pins = target(args.target)
    if run('git', 'status', '--porcelain').strip():
        raise ValueError('expected clean Aegis commit')
    rev = run('git', 'rev-parse', 'HEAD').decode().strip()
    out = args.output.resolve()
    out.mkdir(mode=0o700)  # must be new, below a trusted private parent
    flags = ['-s', '-w']
    pkg = 'github.com/berryhill/aegis/internal/'
    for key, value in sorted(pins.items()):
        flags.extend(['-X', pkg + 'command.hermesReview' + key.title() + '=' + value])
    flags.extend(['-X', pkg + 'buildinfo.Version=scoped-review', '-X', pkg + 'buildinfo.SourceRevision=' + rev])
    run('./scripts/build-source.sh', out/'aegis', '-trimpath', '-ldflags=' + ' '.join(flags), env=dict(os.environ, GOFLAGS='-p=1', GOTOOLCHAIN='go1.26.11', CGO_ENABLED='0', GOOS='linux', GOARCH='amd64', GOMAXPROCS='2', GOMEMLIMIT='512MiB'))
    (out/'aegis').chmod(0o700)
    members = {'HERMES_BASE': (BASE+'\n').encode(), 'AEGIS_REVISION': (rev+'\n').encode()}
    with tempfile.TemporaryDirectory(dir=out) as work:
        work = Path(work)
        run('git', 'init', '-q', cwd=work)
        for name in FILES:
            path = work/name
            path.parent.mkdir(exist_ok=True)
            path.write_bytes(run('git', 'show', BASE+':'+name, cwd=args.hermes))
        for name in ('plain-cli.patch', 'aegis-model-review/__init__.py', 'aegis-model-review/plugin.yaml'):
            text = (HERE/(name+'.in')).read_text()
            for key, value in pins.items():
                text = text.replace('@'+key.upper()+'@', value)
            if name.endswith('.patch'):
                run('git', 'apply', '-', cwd=work, input=text.encode())
            else:
                members['plugins/'+name] = text.encode()
        for name in (*FILES, 'hermes_cli/aegis_review.py'):
            members['client/'+name] = (work/name).read_bytes()
    with tarfile.open(out/'hermes-integration.tar', 'x', format=tarfile.USTAR_FORMAT) as archive:
        for name, content in sorted(members.items()):
            if name.endswith('.py'):
                compile(content, name, 'exec')
            entry = tarfile.TarInfo(name)
            entry.size = len(content)
            entry.mode = 0o644
            archive.addfile(entry, io.BytesIO(content))
    (out/'SHA256SUMS').write_text(''.join(hashlib.sha256((out/n).read_bytes()).hexdigest()+'  '+n+'\n' for n in ('aegis', 'hermes-integration.tar')))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('target', 'hermes', 'output'):
        parser.add_argument(name, type=Path)
    build(parser.parse_args())

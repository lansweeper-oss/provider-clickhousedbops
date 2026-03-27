#!/usr/bin/env python3
"""Strip TF type-annotated description blocks from upjet-generated CRD YAML files.

Run after `make generate`.
"""
import re
import sys
from collections.abc import Iterator
from pathlib import Path

_YAML_ANNOTATED     = re.compile(r'^\s*\([^)]+\) .+$')
_YAML_STRIP_TYPE    = re.compile(r'^\s*\([^)]+\) ')
_YAML_INLINE_DESC   = re.compile(r'^(\s*description: )\([^)]+\) (.+)(\n?)$')
_YAML_BLOCK_DESC    = re.compile(r'^\s*description: \|-\s*$')
_LOWERCASE_START    = re.compile(r'^\s*[a-z]')


def _normalize(s: str) -> str:
    return s.replace('`', '').strip()


def _drop_annotated(lines: list[str], pattern: re.Pattern, strip_type: callable) -> list[str]:
    """Drop a leading type-annotated block (first line + continuations) and seek to its plain duplicate."""
    while lines and pattern.match(lines[0]):
        plain = strip_type(lines[0])
        lines = lines[1:]
        idx = next((i for i, l in enumerate(lines) if _normalize(l) == _normalize(plain)), len(lines))
        if idx < len(lines):
            lines = lines[idx:]
    return lines


def _clean_yaml_body(lines: list[str]) -> Iterator[str]:
    lines = _drop_annotated(lines, _YAML_ANNOTATED,
                            lambda l: _YAML_STRIP_TYPE.sub('', l))
    while lines and _LOWERCASE_START.match(lines[0]):
        lines = lines[1:]
    # Remove consecutive near-duplicates differing only in backtick formatting.
    i = 0
    while i < len(lines):
        if i + 1 < len(lines) and _normalize(lines[i]) == _normalize(lines[i + 1]):
            yield lines[i + 1] if '`' in lines[i + 1] else lines[i]
            i += 2
        else:
            yield lines[i]
            i += 1


def _scan(lines: list[str]) -> Iterator[str]:
    i = 0
    while i < len(lines):
        line = lines[i]
        m = _YAML_INLINE_DESC.match(line)
        if m:
            yield f'{m.group(1)}{m.group(2)}{m.group(3)}'
            i += 1
            continue
        if _YAML_BLOCK_DESC.match(line):
            yield line
            i += 1
            body: list[str] = []
            body_indent: str | None = None
            while i < len(lines):
                bl = lines[i]
                bl_stripped = bl.lstrip()
                if not bl_stripped or bl_stripped.startswith('#'):
                    break
                indent_here = bl[: len(bl) - len(bl_stripped)]
                if body_indent is None:
                    body_indent = indent_here
                elif not bl.startswith(body_indent):
                    break
                body.append(bl.rstrip('\n'))
                i += 1
            yield from (bl + '\n' for bl in _clean_yaml_body(body))
            continue
        yield line
        i += 1


def clean_yaml_file(path: Path) -> None:
    original = path.read_text()
    modified = ''.join(_scan(original.splitlines(keepends=True)))
    if modified != original:
        path.write_text(modified)
        print(f'cleaned {path}')


def main() -> None:
    if len(sys.argv) < 2:
        print(f'Usage: {sys.argv[0]} <file_or_dir> [...]', file=sys.stderr)
        sys.exit(1)
    for arg in sys.argv[1:]:
        p = Path(arg)
        if p.is_dir():
            for f in sorted(p.rglob('*.yaml')):
                clean_yaml_file(f)
        elif p.is_file():
            if p.suffix in ('.yaml', '.yml'):
                clean_yaml_file(p)
        else:
            print(f'warning: {arg} not found', file=sys.stderr)


if __name__ == '__main__':
    main()

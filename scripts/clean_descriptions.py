#!/usr/bin/env python3
"""Clean up resource descriptions around upjet code generation.

  crds <dir>...  Strip TF type-annotated description blocks from
                 upjet-generated CRD YAML files. Run after `make generate`.
  docs <dir>...  Make the front matter description of Terraform docs
                 readable by upjet's scraper. Run on the pulled docs before
                 scraping.

The scraper keeps only the first markdown text node of the front matter, so
the description is cut at any inline markup (`*`, `<tag>`, ...) or blank line
(the HCL examples upstream embeds). `docs` rewrites it as one line without the
HCL, with inline markup as HTML entities, which the scraper decodes back. '&'
is left alone so a re-run is a no-op.
"""
import argparse
import re
from collections.abc import Iterator
from pathlib import Path

_YAML_ANNOTATED = re.compile(r'^\s*\([^)]+\) .+$')
_YAML_STRIP_TYPE = re.compile(r'^\s*\([^)]+\) ')
_YAML_INLINE_DESC = re.compile(r'^(\s*description: )\([^)]+\) (.+)(\n?)$')
_YAML_BLOCK_DESC = re.compile(r'^\s*description: \|-\s*$')
_LOWERCASE_START = re.compile(r'^\s*[a-z]')

_MD_DESC = re.compile(r'^(description: \|-\n)(.*?)^(?=---$)', re.M | re.S)
_MD_HCL_BLOCK = re.compile(r'\{\s*$', re.M)
_MD_ENTITIES = str.maketrans({
    '\\': '&#92;', '*': '&#42;', '_': '&#95;',
    '`': '&#96;', '<': '&lt;', '[': '&#91;',
})


def _normalize(s: str) -> str:
    return s.replace('`', '').strip()


def _drop_annotated(lines: list[str], pattern: re.Pattern,
                    strip_type: callable) -> list[str]:
    """Drop a leading type-annotated block (first line + continuations) and
    seek to its plain duplicate."""
    while lines and pattern.match(lines[0]):
        plain = strip_type(lines[0])
        lines = lines[1:]
        idx = next((i for i, line in enumerate(lines)
                    if _normalize(line) == _normalize(plain)), len(lines))
        if idx < len(lines):
            lines = lines[idx:]
    return lines


def _clean_yaml_body(lines: list[str]) -> Iterator[str]:
    lines = _drop_annotated(lines, _YAML_ANNOTATED,
                            lambda line: _YAML_STRIP_TYPE.sub('', line))
    while lines and _LOWERCASE_START.match(lines[0]):
        lines = lines[1:]
    # Remove consecutive near-duplicates differing only in backtick formatting.
    i = 0
    while i < len(lines):
        if (i + 1 < len(lines)
                and _normalize(lines[i]) == _normalize(lines[i + 1])):
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


def _clean_md_desc(m: re.Match) -> str:
    lines = (
        line.strip()
        for chunk in re.split(r'\n\s*\n', m[2])
        if not _MD_HCL_BLOCK.search(chunk)
        for line in chunk.splitlines()
    )
    prose = ' '.join(
        line for line in lines if line not in ('', 'Example Usage'))
    return f'{m[1]}  {prose.translate(_MD_ENTITIES)}\n'


def clean_doc(text: str) -> str:
    return _MD_DESC.sub(_clean_md_desc, text, count=1)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('command', choices=['crds', 'docs'])
    parser.add_argument('dirs', nargs='+', type=Path)
    args = parser.parse_args()
    for d in args.dirs:
        if args.command == 'crds':
            for f in sorted(d.rglob('*.yaml')):
                clean_yaml_file(f)
        else:
            for f in d.glob('*.md'):
                f.write_text(clean_doc(f.read_text()))

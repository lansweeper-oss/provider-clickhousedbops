#!/usr/bin/env python3
"""
Clean generated field descriptions in upjet output.

upjet generates two blocks per field in Go type files:

  // (String) Line 1 of description.   <- TF-annotated block, first line
  // Line 2 (continuation).            <- continuation of annotated block
  // Line 1 of description.            <- plain duplicate block (keep)
  // Line 2 (continuation).            <- continuation of plain block (keep)

controller-gen propagates these into CRD YAML descriptions:

  (String) Line 1 of description.   <- TF-annotated block, first line
  Line 2 (continuation).            <- continuation of annotated block
  Line 1 of description.            <- plain duplicate block (keep)
  Line 2 (continuation).            <- continuation of plain block (keep)

The annotated block is always removed in full: the script finds where the
plain duplicate begins (the first line that matches the annotated line minus
its type prefix) and discards everything before it.

Run this script after `make generate` to strip the annotated blocks.
"""
import re
import sys
from pathlib import Path

# Matches a Go comment line that opens with a TF type annotation, e.g.:
#   \t// (String, Sensitive) some description
_GO_TYPE_LINE = re.compile(r'^\s*// \([^)]+\) .+$')

# Matches a YAML body line that opens with a TF type annotation, e.g.:
#   "  (Boolean) If true..."
_YAML_TYPE_LINE = re.compile(r'^\s*\([^)]+\) .+$')


def _normalize(s: str) -> str:
    """Strip backtick formatting and whitespace for comparison purposes."""
    return s.replace('`', '').strip()


# ---------------------------------------------------------------------------
# Go file cleaning
# ---------------------------------------------------------------------------

def _clean_go_block(block: list[str]) -> list[str]:
    """
    Remove TF-annotated blocks from a consecutive comment block.

    upjet emits an annotated block (starting with ``// (Type) ...``) followed
    by a plain duplicate.  The annotated block may span multiple lines
    (continuation lines have the same indentation but no type prefix).
    The entire annotated block — first line plus all continuations — is dropped
    by seeking forward to the first line that matches the plain duplicate of
    the annotated first line.
    """
    while block and _GO_TYPE_LINE.match(block[0]):
        # Derive what the plain duplicate of this annotated line looks like.
        plain_first = re.sub(r'^(\s*// )\([^)]+\) ', r'\1', block[0])
        block = block[1:]
        # Skip continuation lines until the plain duplicate is found.
        skip = 0
        while skip < len(block) and _normalize(block[skip]) != _normalize(plain_first):
            skip += 1
        if skip < len(block):
            block = block[skip:]

    # Drop a leading lowercase fragment line (orphaned URL continuation).
    if len(block) >= 2 and re.match(r'^\s*// [a-z]', block[0]):
        block = block[1:]

    return block


def clean_go_file(path: Path) -> bool:
    """Remove TF-annotated comment lines from a Go types file."""
    original = path.read_text()
    lines = original.splitlines(keepends=True)

    result: list[str] = []
    i = 0
    while i < len(lines):
        line = lines[i]
        stripped = line.lstrip()
        if not stripped.startswith('//'):
            result.append(line)
            i += 1
            continue

        # Collect consecutive comment lines sharing the same leading indentation.
        indent = line[: len(line) - len(stripped)]
        block: list[str] = []
        while i < len(lines):
            l = lines[i]
            if l.lstrip().startswith('//') and l.startswith(indent):
                block.append(l.rstrip('\n'))
                i += 1
            else:
                break

        for cl in _clean_go_block(block):
            result.append(cl + '\n')

    modified = ''.join(result)
    if modified != original:
        path.write_text(modified)
        print(f'cleaned {path}')
        return True
    return False


# ---------------------------------------------------------------------------
# CRD YAML file cleaning
# ---------------------------------------------------------------------------

def _clean_yaml_body(body_lines: list[str]) -> list[str]:
    """
    Remove TF-annotated blocks from a YAML description body.

    1. Drop leading annotated blocks (starting with ``(Type) ...``) together
       with their continuation lines, by seeking to the plain duplicate.
    2. Drop consecutive near-duplicate lines where one is the same as the
       other but without backtick formatting (keep the backtick version).
    """
    # Step 1: remove type-annotated leading blocks and their continuations.
    while body_lines and _YAML_TYPE_LINE.match(body_lines[0]):
        plain_first = re.sub(r'^\s*\([^)]+\) ', '', body_lines[0])
        body_lines = body_lines[1:]
        skip = 0
        while skip < len(body_lines) and _normalize(body_lines[skip]) != _normalize(plain_first):
            skip += 1
        if skip < len(body_lines):
            body_lines = body_lines[skip:]

    # Also remove orphaned lowercase continuation lines (e.g. wrapped URLs).
    while body_lines and re.match(r'^\s*[a-z]', body_lines[0]):
        body_lines = body_lines[1:]

    # Step 2: remove near-duplicate consecutive lines (backtick vs plain).
    result: list[str] = []
    i = 0
    while i < len(body_lines):
        if (i + 1 < len(body_lines)
                and _normalize(body_lines[i]) == _normalize(body_lines[i + 1])):
            # The two lines are the same when backticks are ignored.
            # Keep the one that contains backticks (the formatted version).
            if '`' in body_lines[i + 1]:
                i += 1  # skip current (plain), keep next (backtick-formatted)
            else:
                result.append(body_lines[i])
                i += 2  # skip next (plain duplicate)
            continue
        result.append(body_lines[i])
        i += 1
    return result


def clean_yaml_file(path: Path) -> bool:
    """Remove TF-annotated duplicate lines from CRD YAML description fields."""
    original = path.read_text()
    lines = original.splitlines(keepends=True)

    result: list[str] = []
    i = 0
    while i < len(lines):
        line = lines[i]

        # Inline description starting with (Type): `description: (String) foo`
        m = re.match(r'^(\s*description: )\([^)]+\) (.+)(\n?)$', line)
        if m:
            result.append(f'{m.group(1)}{m.group(2)}{m.group(3)}')
            i += 1
            continue

        # Multi-line description block: `description: |-`
        if re.match(r'^\s*description: \|-\s*$', line):
            result.append(line)
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
            for bl in _clean_yaml_body(body):
                result.append(bl + '\n')
            continue

        result.append(line)
        i += 1

    modified = ''.join(result)
    if modified != original:
        path.write_text(modified)
        print(f'cleaned {path}')
        return True
    return False


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main() -> None:
    if len(sys.argv) < 2:
        print(f'Usage: {sys.argv[0]} <file_or_dir> [...]', file=sys.stderr)
        sys.exit(1)

    for arg in sys.argv[1:]:
        p = Path(arg)
        if p.is_dir():
            for f in sorted(p.rglob('zz_*_types.go')):
                clean_go_file(f)
            for f in sorted(p.rglob('*.yaml')):
                clean_yaml_file(f)
        elif p.is_file():
            if p.suffix == '.go':
                clean_go_file(p)
            elif p.suffix in ('.yaml', '.yml'):
                clean_yaml_file(p)
        else:
            print(f'warning: {arg} not found', file=sys.stderr)


if __name__ == '__main__':
    main()

#!/usr/bin/env python3
"""Check this kit's local links, skill entrypoints and discovery links; no network."""

import re
import sys
import tempfile
from pathlib import Path
from urllib.parse import unquote, urlsplit


SKILLS = ("gk-plan", "gk-feature", "gk-plan-review")


def check_document(path):
    text = path.read_text(encoding="utf-8")
    errors = []
    if path.name == "SKILL.md":
        # ponytail: two plain scalar fields suffice here; use YAML if metadata grows.
        header = re.match(r"\A---\nname: ([a-z0-9-]+)\ndescription: ([^\n]+)\n---\n", text)
        if not header or header[1] != path.parent.name or not header[2].strip():
            errors.append("invalid skill name/description frontmatter")
    # ponytail: inline links without spaces only; adopt a Markdown parser if syntax expands.
    prose = re.sub(r"(?ms)^```[^\n]*\n.*?^```\s*$", "", text)
    for target in re.findall(r"\[[^\]\n]+\]\(([^\s)]+)\)", prose):
        url = urlsplit(target)
        if url.scheme or url.netloc or not url.path:
            continue
        destination = path.parent / unquote(url.path)
        if not destination.exists():
            errors.append(f"missing local link: {target}")
    for number, line in enumerate(text.splitlines(), 1):
        if line.rstrip() != line:
            errors.append(f"trailing whitespace at line {number}")
    return errors


def check_kit(root):
    documents = [root / name for name in ("README.md", "SETUP.md", "AGENTS.md")]
    documents += sorted((root / "docs/planning").glob("*.md"))
    errors = []
    for name in SKILLS:
        skill = root / "skills" / name
        documents.append(skill / "SKILL.md")
        documents += sorted((skill / "references").glob("*.md"))
        entry = root / ".agents/skills" / name
        if not entry.is_symlink() or entry.resolve() != skill.resolve():
            errors.append(f"{entry.relative_to(root)}: missing/incorrect discovery link")
    for path in documents:
        if not path.is_file():
            errors.append(f"{path.relative_to(root)}: missing document")
            continue
        errors += [f"{path.relative_to(root)}: {error}" for error in check_document(path)]
    return errors


def self_test():
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        skill = root / "example"
        skill.mkdir()
        document = skill / "SKILL.md"
        document.write_text("---\nname: example\ndescription: Check links\n---\n", encoding="utf-8")
        assert check_document(document) == []
        with document.open("a", encoding="utf-8") as handle:
            handle.write("[missing](guide.md)\n[web](https://example.com)\n[here](#anchor)\n")
        assert check_document(document) == ["missing local link: guide.md"]
        (skill / "guide.md").write_text("# Guide\n", encoding="utf-8")
        assert check_document(document) == []
        document.write_text("---\nname: wrong\ndescription: Check\n---\n", encoding="utf-8")
        assert check_document(document) == ["invalid skill name/description frontmatter"]
        assert check_kit(root), "An empty kit must fail validation"
    print("Checker self-test passed")


if __name__ == "__main__":
    if sys.argv[1:] == ["--self-test"]:
        self_test()
    elif sys.argv[1:]:
        sys.exit("Usage: check_planning_kit.py [--self-test]")
    else:
        failures = check_kit(Path(__file__).resolve().parents[1])
        if failures:
            sys.exit("\n".join(failures))
        print("Planning kit checks passed")
